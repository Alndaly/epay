package alipay

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"epay/internal/model"
	"epay/internal/provider"
	"epay/internal/provider/keyutil"
)

// newTestAlipay 使用随机生成的密钥对：appKey 模拟应用私钥，alipayKey 模拟支付宝私钥（用于伪造通知签名）。
func newTestAlipay(t *testing.T, mode string) (*Alipay, *rsa.PrivateKey) {
	t.Helper()
	appKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	alipayKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	privDER, _ := x509.MarshalPKCS8PrivateKey(appKey)
	pubDER, _ := x509.MarshalPKIXPublicKey(&alipayKey.PublicKey)
	a, err := New(Config{
		AppID:           "2021000000000000",
		PrivateKey:      base64.StdEncoding.EncodeToString(privDER), // 支付宝工具导出的裸 Base64 格式
		AlipayPublicKey: base64.StdEncoding.EncodeToString(pubDER),
		Mode:            mode,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a, alipayKey
}

func TestPagePayURL(t *testing.T) {
	a, _ := newTestAlipay(t, "auto")
	res, err := a.Pay(t.Context(), &provider.PayRequest{
		Order:     &model.Order{TradeNo: "T1", Name: "商品", Money: 1234},
		NotifyURL: "https://gw/notify/alipay", ReturnURL: "https://gw/return/T1",
		Device: provider.DevicePC,
	})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(res.Content)
	q := u.Query()
	if res.Kind != model.PayKindRedirect || q.Get("method") != "alipay.trade.page.pay" ||
		!strings.Contains(q.Get("biz_content"), `"total_amount":"12.34"`) {
		t.Fatalf("unexpected pay url: %s", res.Content)
	}
	// 用应用公钥校验请求签名
	params := map[string]string{}
	for k := range q {
		params[k] = q.Get(k)
	}
	if err := keyutil.Verify(&a.priv.PublicKey, []byte(signContent(params, false)), q.Get("sign")); err != nil {
		t.Fatalf("request signature invalid: %v", err)
	}
}

func TestParseNotify(t *testing.T) {
	a, alipayKey := newTestAlipay(t, "page")
	params := map[string]string{
		"app_id": a.cfg.AppID, "out_trade_no": "T1", "trade_no": "2024001", "trade_status": "TRADE_SUCCESS",
		"total_amount": "12.34", "buyer_logon_id": "a***@b.com", "sign_type": "RSA2",
	}
	sig, _ := keyutil.Sign(alipayKey, []byte(signContent(params, true)))
	form := url.Values{"sign": {sig}}
	for k, v := range params {
		form.Set(k, v)
	}

	req := httptest.NewRequest(http.MethodPost, "/notify/alipay", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	p, err := a.ParseNotify(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Paid || p.Amount != 1234 || p.TradeNo != "T1" || p.APITradeNo != "2024001" {
		t.Fatalf("unexpected payment: %+v", p)
	}

	form.Set("total_amount", "0.01") // 篡改金额
	req = httptest.NewRequest(http.MethodPost, "/notify/alipay", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := a.ParseNotify(t.Context(), req); err == nil {
		t.Fatal("tampered notify should fail")
	}
}
