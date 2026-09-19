package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	goepay "github.com/Calcium-Ion/go-epay/epay"

	"epay/internal/epay"
)

// TestNewAPICompat 使用 new-api 实际依赖的易支付客户端 go-epay 验证协议兼容性：
// 客户端生成的下单参数能被网关接受，网关发出的通知能通过客户端验签。
func TestNewAPICompat(t *testing.T) {
	verified := make(chan *goepay.VerifyRes, 1)
	client, _ := goepay.NewClient(&goepay.Config{PartnerID: testPID, Key: testKey}, "http://gateway.test")

	// 模拟 new-api 的 /api/user/epay/notify
	newapi := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res, err := client.Verify(epay.FromValues(r.URL.Query()))
		if err != nil || !res.VerifyStatus {
			http.Error(w, "fail", http.StatusBadRequest)
			return
		}
		verified <- res
		io.WriteString(w, "success")
	}))
	defer newapi.Close()

	h := newTestHandler(t)

	notifyURL, _ := url.Parse(newapi.URL + "/api/user/epay/notify")
	returnURL, _ := url.Parse("http://newapi.test/console/log")
	submitURL, params, err := client.Purchase(&goepay.PurchaseArgs{
		Type:           "mock",
		ServiceTradeNo: "USR1NOabc123",
		Name:           "TUC10",
		Money:          "7.30",
		Device:         goepay.PC,
		NotifyUrl:      notifyURL,
		ReturnUrl:      returnURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(submitURL, "/submit.php") {
		t.Fatalf("unexpected submit url %s", submitURL)
	}

	// new-api 前端以表单 POST 提交这些参数
	rec := do(h, http.MethodPost, "/submit.php", epay.ToValues(params))
	if rec.Code != http.StatusFound {
		t.Fatalf("submit rejected: %d %s", rec.Code, rec.Body)
	}
	tradeNo := strings.TrimPrefix(rec.Header().Get("Location"), "/pay/")
	do(h, http.MethodPost, "/notify/mock", url.Values{"trade_no": {tradeNo}})

	select {
	case res := <-verified:
		if res.TradeStatus != goepay.StatusTradeSuccess || res.ServiceTradeNo != "USR1NOabc123" || res.Money != "7.30" {
			t.Fatalf("unexpected verify result: %+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("new-api was not notified")
	}
}
