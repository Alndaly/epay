package admin

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"epay/internal/epay"
	"epay/internal/gateway"
	_ "epay/internal/provider/mock"
	_ "epay/internal/provider/stripe"
	"epay/internal/server"
	"epay/internal/store/sqlite"
)

type testEnv struct {
	t      *testing.T
	h      http.Handler
	cookie *http.Cookie
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, err := gateway.New(t.Context(), st, gateway.Options{BaseURL: "https://pay.test", Logger: log})
	if err != nil {
		t.Fatal(err)
	}
	adm, err := New(t.Context(), svc, st, Options{Username: "admin", Password: "password123", Logger: log})
	if err != nil {
		t.Fatal(err)
	}
	return &testEnv{t: t, h: server.New(svc, log, server.Options{Admin: adm})}
}

// call 发起 JSON 请求并解析 data 字段，返回状态码。
func (e *testEnv) call(method, path string, body, out any) int {
	e.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if e.cookie != nil {
		req.AddCookie(e.cookie)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName {
			e.cookie = c
		}
	}
	var resp struct {
		Data  json.RawMessage `json:"data"`
		Error string          `json:"error"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if out != nil && resp.Data != nil {
		json.Unmarshal(resp.Data, out)
	}
	if out, isStr := out.(*string); isStr && resp.Error != "" {
		*out = resp.Error
	}
	return rec.Code
}

func (e *testEnv) login() {
	e.t.Helper()
	if code := e.call("POST", "/admin/api/login", map[string]string{"username": "admin", "password": "password123"}, nil); code != 200 {
		e.t.Fatalf("login failed: %d", code)
	}
}

func TestAuth(t *testing.T) {
	e := newEnv(t)
	if code := e.call("GET", "/admin/api/overview", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated request should be rejected, got %d", code)
	}
	if code := e.call("POST", "/admin/api/login", map[string]string{"username": "admin", "password": "wrong"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("wrong password should be rejected, got %d", code)
	}
	e.login()
	if code := e.call("GET", "/admin/api/overview", nil, nil); code != 200 {
		t.Fatalf("overview: %d", code)
	}

	// 非 JSON 的写请求（跨站表单）必须被拒绝
	req := httptest.NewRequest("POST", "/admin/api/merchants", strings.NewReader("name=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(e.cookie)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("form post should be rejected, got %d", rec.Code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t)
	for range loginMaxFails {
		e.call("POST", "/admin/api/login", map[string]string{"username": "admin", "password": "wrong"}, nil)
	}
	var msg string
	e.call("POST", "/admin/api/login", map[string]string{"username": "admin", "password": "password123"}, &msg)
	if !strings.Contains(msg, "次数过多") {
		t.Fatalf("should be rate limited, got %q", msg)
	}
}

func TestChannelSecretsAreMaskedAndPreserved(t *testing.T) {
	e := newEnv(t)
	e.login()

	var created channelView
	code := e.call("POST", "/admin/api/channels", map[string]any{
		"type": "stripe", "driver": "stripe", "enabled": true,
		"options": map[string]any{"secret_key": "sk_test_123", "webhook_secret": "whsec_abc", "exchange_rate": "0.14"},
	}, &created)
	if code != 200 || created.Error != "" {
		t.Fatalf("create channel: %d %+v", code, created)
	}
	var opts map[string]any
	json.Unmarshal(created.Options, &opts)
	if opts["secret_key"] != secretMask || opts["exchange_rate"] != 0.14 {
		t.Fatalf("secret should be masked and number coerced: %v", opts)
	}
	if created.NotifyURL != "https://pay.test/notify/stripe" {
		t.Fatalf("notify url: %s", created.NotifyURL)
	}

	// 提交占位符表示保持原值；修改其他字段后配置仍然有效
	opts["exchange_rate"] = 0.15
	var updated channelView
	code = e.call("PUT", "/admin/api/channels/1", map[string]any{
		"type": "stripe", "driver": "stripe", "enabled": true, "options": opts,
	}, &updated)
	if code != 200 || updated.Error != "" {
		t.Fatalf("update channel: %d %+v", code, updated)
	}

	// 缺少必填项时保存失败，并返回可读的原因
	var msg string
	code = e.call("POST", "/admin/api/channels", map[string]any{
		"type": "stripe2", "driver": "stripe", "options": map[string]any{"secret_key": "sk"},
	}, &msg)
	if code != http.StatusBadRequest || !strings.Contains(msg, "不能为空") {
		t.Fatalf("invalid channel should be rejected: %d %q", code, msg)
	}

	// type 创建后不可修改
	code = e.call("PUT", "/admin/api/channels/1", map[string]any{"type": "other", "driver": "stripe", "options": opts}, &msg)
	if code != http.StatusBadRequest {
		t.Fatalf("changing type should be rejected: %d", code)
	}
}

// TestHotReload 在后台新建商户与渠道后立即可以下单；停用渠道后拒绝新订单。
func TestHotReload(t *testing.T) {
	e := newEnv(t)
	e.login()

	var m merchantView
	if code := e.call("POST", "/admin/api/merchants", map[string]string{"name": "new-api"}, &m); code != 200 || m.PID != "1001" || len(m.Key) != 32 {
		t.Fatalf("create merchant: %d %+v", code, m)
	}
	var list []merchantView
	e.call("GET", "/admin/api/merchants", nil, &list)
	if len(list) != 1 || list[0].Key != "" {
		t.Fatalf("merchant list must not expose key: %+v", list)
	}
	if code := e.call("POST", "/admin/api/channels", map[string]any{"type": "mock", "driver": "mock", "enabled": true}, nil); code != 200 {
		t.Fatalf("create channel: %d", code)
	}

	submit := func(outTradeNo string) *httptest.ResponseRecorder {
		params := epay.SignParams(map[string]string{
			"pid": m.PID, "type": "mock", "out_trade_no": outTradeNo, "notify_url": "http://m.test/n",
			"name": "n", "money": "1.00",
		}, m.Key)
		req := httptest.NewRequest("POST", "/submit.php", strings.NewReader(epay.ToValues(params).Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		return rec
	}
	rec := submit("A1")
	if rec.Code != http.StatusFound {
		t.Fatalf("order should be accepted after hot reload: %d", rec.Code)
	}
	tradeNo := strings.TrimPrefix(rec.Header().Get("Location"), "/pay/")

	e.call("PUT", "/admin/api/channels/1/enabled", map[string]bool{"enabled": false}, nil)
	if rec := submit("A2"); rec.Code != http.StatusBadRequest {
		t.Fatalf("disabled channel should reject new orders: %d", rec.Code)
	}

	// 停用的渠道仍然处理存量订单的回调
	req := httptest.NewRequest("POST", "/notify/mock", strings.NewReader(url.Values{"trade_no": {tradeNo}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	nrec := httptest.NewRecorder()
	e.h.ServeHTTP(nrec, req)
	if nrec.Body.String() != "success" {
		t.Fatalf("disabled channel should still accept callbacks: %s", nrec.Body)
	}

	var page struct {
		Items []orderView `json:"items"`
		Total int         `json:"total"`
	}
	e.call("GET", "/admin/api/orders?status=paid", nil, &page)
	if page.Total != 1 || page.Items[0].Status != "paid" || page.Items[0].NotifyStatus != "pending" {
		t.Fatalf("orders: %+v", page)
	}
	var o orderView
	if code := e.call("POST", "/admin/api/orders/"+tradeNo+"/renotify", nil, &o); code != 200 || o.NotifyCount != 0 {
		t.Fatalf("renotify: %d %+v", code, o)
	}
}
