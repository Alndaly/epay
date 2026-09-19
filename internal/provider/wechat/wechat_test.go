package wechat

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"epay/internal/provider/keyutil"
)

const apiV3Key = "0123456789abcdef0123456789abcdef"

func newTestWechat(t *testing.T) (*Wechat, *rsa.PrivateKey) {
	t.Helper()
	merchantKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	platformKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	privDER, _ := x509.MarshalPKCS8PrivateKey(merchantKey)
	pubDER, _ := x509.MarshalPKIXPublicKey(&platformKey.PublicKey)

	w, err := New(Config{
		AppID: "wx1", MchID: "1900000001", APIv3Key: apiV3Key, SerialNo: "SERIAL",
		PrivateKey:  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})),
		PublicKeyID: "PUB_KEY_ID_1",
		PublicKey:   string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})),
	})
	if err != nil {
		t.Fatal(err)
	}
	return w, platformKey
}

func encrypt(t *testing.T, plain, nonce, ad string) string {
	block, _ := aes.NewCipher([]byte(apiV3Key))
	gcm, _ := cipher.NewGCM(block)
	return base64.StdEncoding.EncodeToString(gcm.Seal(nil, []byte(nonce), []byte(plain), []byte(ad)))
}

func TestParseNotify(t *testing.T) {
	w, platformKey := newTestWechat(t)

	txn := `{"out_trade_no":"T1","transaction_id":"42000","trade_state":"SUCCESS","amount":{"total":100,"currency":"CNY"},"payer":{"openid":"o1"}}`
	body := `{"event_type":"TRANSACTION.SUCCESS","resource":{"algorithm":"AEAD_AES_256_GCM","ciphertext":"` +
		encrypt(t, txn, "abcdefghijkl", "transaction") + `","associated_data":"transaction","nonce":"abcdefghijkl"}}`

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig, _ := keyutil.Sign(platformKey, []byte(ts+"\nnonce\n"+body+"\n"))

	req := httptest.NewRequest(http.MethodPost, "/notify/wxpay", strings.NewReader(body))
	req.Header.Set("Wechatpay-Serial", "PUB_KEY_ID_1")
	req.Header.Set("Wechatpay-Timestamp", ts)
	req.Header.Set("Wechatpay-Nonce", "nonce")
	req.Header.Set("Wechatpay-Signature", sig)

	p, err := w.ParseNotify(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Paid || p.TradeNo != "T1" || p.Amount != 100 || p.APITradeNo != "42000" {
		t.Fatalf("unexpected payment: %+v", p)
	}

	// 篡改报文后验签必须失败
	req = httptest.NewRequest(http.MethodPost, "/notify/wxpay", strings.NewReader(strings.Replace(body, "SUCCESS", "SUCCESX", 1)))
	req.Header = req.Header.Clone()
	req.Header.Set("Wechatpay-Serial", "PUB_KEY_ID_1")
	req.Header.Set("Wechatpay-Timestamp", ts)
	req.Header.Set("Wechatpay-Nonce", "nonce")
	req.Header.Set("Wechatpay-Signature", sig)
	if _, err := w.ParseNotify(t.Context(), req); err == nil {
		t.Fatal("tampered notify should fail")
	}
}
