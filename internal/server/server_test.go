package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"epay/internal/epay"
	"epay/internal/gateway"
	"epay/internal/provider"
	_ "epay/internal/provider/mock"
	"epay/internal/store/sqlite"
)

const (
	testPID = "1001"
	testKey = "test-merchant-key-123456"
)

// TestEndToEnd 模拟 new-api 的完整接入流程：
// submit.php 下单 → 收银台 → 上游回调 → 商户异步通知 → 同步跳转 → 查单 → 退款。
func TestEndToEnd(t *testing.T) {
	// 模拟商户（new-api）的通知接收端：验签并返回 success。
	notified := make(chan url.Values, 1)
	merchant := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !epay.Verify(epay.FromValues(r.URL.Query()), testKey) {
			http.Error(w, "bad sign", http.StatusBadRequest)
			return
		}
		notified <- r.URL.Query()
		io.WriteString(w, "success")
	}))
	defer merchant.Close()

	h := newTestHandler(t)

	// 1. 下单
	form := signedOrder("ORDER-1", "12.34", merchant.URL+"/notify")
	rec := do(h, http.MethodPost, "/submit.php", form)
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/pay/") {
		t.Fatalf("submit: code=%d location=%q body=%s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	cashier := rec.Header().Get("Location")
	tradeNo := strings.TrimPrefix(cashier, "/pay/")

	// 2. 收银台展示二维码
	rec = do(h, http.MethodGet, cashier, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "qrcode.png") {
		t.Fatalf("cashier: code=%d", rec.Code)
	}
	if rec := do(h, http.MethodGet, cashier+"/qrcode.png", nil); rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("qrcode: code=%d", rec.Code)
	}

	// 3. 重复提交同一商户订单号应复用原订单
	rec = do(h, http.MethodPost, "/submit.php", form)
	if rec.Header().Get("Location") != cashier {
		t.Fatalf("idempotent submit: location=%q", rec.Header().Get("Location"))
	}

	// 4. 上游回调（模拟渠道）
	rec = do(h, http.MethodPost, "/notify/mock", url.Values{"trade_no": {tradeNo}})
	if rec.Body.String() != "success" {
		t.Fatalf("upstream notify: %d %s", rec.Code, rec.Body)
	}

	// 5. 商户收到签名正确的异步通知
	select {
	case q := <-notified:
		if q.Get("trade_status") != epay.TradeSuccess || q.Get("out_trade_no") != "ORDER-1" ||
			q.Get("money") != "12.34" || q.Get("trade_no") != tradeNo || q.Get("param") != "uid=1" {
			t.Fatalf("unexpected notify params: %v", q)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("merchant was not notified")
	}

	// 6. 同步跳转回商户 return_url，且带签名
	rec = do(h, http.MethodGet, "/return/"+tradeNo, nil)
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusFound || loc.Host != "merchant.test" || !epay.Verify(epay.FromValues(loc.Query()), testKey) {
		t.Fatalf("return: code=%d location=%v", rec.Code, loc)
	}

	// 7. 查询订单
	var order map[string]any
	rec = do(h, http.MethodGet, "/api.php?act=order&pid="+testPID+"&key="+testKey+"&out_trade_no=ORDER-1", nil)
	json.Unmarshal(rec.Body.Bytes(), &order)
	if order["code"] != float64(1) || order["status"] != float64(1) || order["money"] != "12.34" {
		t.Fatalf("query order: %s", rec.Body)
	}

	// 8. 退款：部分退款成功，超额退款失败
	refund := func(amount string) map[string]any {
		var resp map[string]any
		rec := do(h, http.MethodPost, "/api.php", url.Values{
			"act": {"refund"}, "pid": {testPID}, "key": {testKey}, "trade_no": {tradeNo}, "money": {amount},
		})
		json.Unmarshal(rec.Body.Bytes(), &resp)
		return resp
	}
	if r := refund("10.00"); r["code"] != float64(1) {
		t.Fatalf("refund: %v", r)
	}
	if r := refund("5.00"); r["code"] == float64(1) {
		t.Fatalf("over-refund should fail: %v", r)
	}

	// 9. 已支付订单不能再次下单
	rec = do(h, http.MethodPost, "/submit.php", form)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "已支付") {
		t.Fatalf("paid order resubmit: %d", rec.Code)
	}
}

func TestSubmitRejectsBadRequests(t *testing.T) {
	h := newTestHandler(t)

	form := signedOrder("ORDER-2", "1.00", "")
	form.Set("money", "100.00") // 篡改金额
	if rec := do(h, http.MethodPost, "/submit.php", form); !strings.Contains(rec.Body.String(), "签名校验失败") {
		t.Fatalf("tampered order should be rejected: %s", rec.Body)
	}

	var resp map[string]any
	form = signedOrder("ORDER-3", "1.00", "")
	form.Set("type", "unknown")
	form.Set("sign", epay.Sign(epay.FromValues(form), testKey))
	rec := do(h, http.MethodPost, "/mapi.php", form)
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["code"] != float64(-1) || !strings.Contains(resp["msg"].(string), "不支持") {
		t.Fatalf("unknown type should be rejected: %s", rec.Body)
	}
}

func TestMAPI(t *testing.T) {
	h := newTestHandler(t)
	var resp map[string]any
	rec := do(h, http.MethodPost, "/mapi.php", signedOrder("ORDER-4", "0.01", ""))
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["code"] != float64(1) || resp["qrcode"] == "" || resp["trade_no"] == "" {
		t.Fatalf("mapi: %s", rec.Body)
	}
}

// ---- helpers ----

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	mock, err := provider.New("mock", nil)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, err := gateway.New(st, gateway.Options{
		BaseURL:   "http://gateway.test",
		Merchants: []gateway.Merchant{{PID: testPID, Key: testKey, Name: "test"}},
		Channels:  []gateway.Channel{{Type: "mock", Name: "模拟支付", Provider: mock}},
		Logger:    log,
	})
	if err != nil {
		t.Fatal(err)
	}
	go svc.RunNotifier(t.Context())
	return New(svc, log, false)
}

func signedOrder(outTradeNo, amount, notifyURL string) url.Values {
	if notifyURL == "" {
		notifyURL = "http://merchant.test/notify"
	}
	params := map[string]string{
		"pid": testPID, "type": "mock", "out_trade_no": outTradeNo,
		"notify_url": notifyURL, "return_url": "http://merchant.test/return",
		"name": "测试商品", "money": amount, "param": "uid=1",
	}
	return epay.ToValues(epay.SignParams(params, testKey))
}

func do(h http.Handler, method, target string, form url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
