// Package epay 实现"易支付"协议的签名规则与常量，不依赖任何业务代码。
//
// 签名算法（MD5）：
//  1. 去掉 sign、sign_type 以及值为空的参数；
//  2. 按参数名 ASCII 升序排序，拼接成 a=1&b=2 形式（值不做 URL 编码）；
//  3. 在末尾直接追加商户密钥 KEY，取 MD5 的 32 位小写十六进制。
package epay

import (
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
)

const (
	SignTypeMD5  = "MD5"
	TradeSuccess = "TRADE_SUCCESS"
)

// Sign 计算参数签名。
func Sign(params map[string]string, key string) string {
	sum := md5.Sum([]byte(signContent(params) + key))
	return hex.EncodeToString(sum[:])
}

// Verify 校验参数中的 sign 字段，使用常量时间比较避免时序攻击。
func Verify(params map[string]string, key string) bool {
	got := strings.ToLower(params["sign"])
	if got == "" {
		return false
	}
	want := Sign(params, key)
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// SignParams 返回添加了 sign 与 sign_type 的新参数表，并移除空值参数，
// 保证"发出去的参数"与"参与签名的参数"完全一致。
func SignParams(params map[string]string, key string) map[string]string {
	out := make(map[string]string, len(params)+2)
	for k, v := range params {
		if v != "" && k != "sign" && k != "sign_type" {
			out[k] = v
		}
	}
	out["sign"] = Sign(out, key)
	out["sign_type"] = SignTypeMD5
	return out
}

// FromValues 将 url.Values 转为单值 map（同名参数取第一个）。
func FromValues(v url.Values) map[string]string {
	m := make(map[string]string, len(v))
	for k := range v {
		m[k] = v.Get(k)
	}
	return m
}

// ToValues 将单值 map 转为 url.Values，便于拼接查询串。
func ToValues(m map[string]string) url.Values {
	v := make(url.Values, len(m))
	for k, val := range m {
		v.Set(k, val)
	}
	return v
}

func signContent(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || k == "sign_type" || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(params[k])
	}
	return b.String()
}
