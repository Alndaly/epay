package epay

import (
	"crypto/md5"
	"encoding/hex"
	"testing"
)

func TestSign(t *testing.T) {
	params := map[string]string{
		"pid": "1001", "type": "alipay", "out_trade_no": "A1", "money": "1.00",
		"name": "测试", "empty": "", "sign": "ignored", "sign_type": "MD5",
	}
	raw := "money=1.00&name=测试&out_trade_no=A1&pid=1001&type=alipayKEY"
	sum := md5.Sum([]byte(raw))
	if got, want := Sign(params, "KEY"), hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("Sign = %s, want %s", got, want)
	}
}

func TestSignParamsAndVerify(t *testing.T) {
	signed := SignParams(map[string]string{"a": "1", "b": "", "c": "3"}, "k")
	if _, ok := signed["b"]; ok {
		t.Fatal("empty params should be removed")
	}
	if signed["sign_type"] != SignTypeMD5 || !Verify(signed, "k") {
		t.Fatal("signed params should verify")
	}
	signed["a"] = "2"
	if Verify(signed, "k") {
		t.Fatal("tampered params should not verify")
	}
}
