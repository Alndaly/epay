package stripe

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"
)

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"type":"checkout.session.completed"}`)
	now := time.Now()
	ts := strconv.FormatInt(now.Unix(), 10)
	mac := hmac.New(sha256.New, []byte("whsec_test"))
	mac.Write([]byte(ts + "." + string(body)))
	header := "t=" + ts + ",v1=deadbeef,v1=" + hex.EncodeToString(mac.Sum(nil))

	if err := verifySignature(header, body, "whsec_test", now); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := verifySignature(header, body, "whsec_other", now); err == nil {
		t.Fatal("wrong secret should fail")
	}
	if err := verifySignature(header, body, "whsec_test", now.Add(10*time.Minute)); err == nil {
		t.Fatal("expired signature should fail")
	}
}
