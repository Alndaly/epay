// Package keyutil 解析 RSA 密钥并提供 SHA256withRSA 签名/验签，
// 供支付宝（RSA2）与微信支付 APIv3 共用。
//
// 配置中的密钥既可以直接填写内容（PEM 或去掉头尾的 Base64），也可以填写文件路径。
package keyutil

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
)

// load 若 s 是存在的文件路径则读取文件，否则将 s 视为密钥内容本身。
func load(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("密钥为空")
	}
	if !strings.Contains(s, "-----BEGIN") && len(s) < 512 {
		if info, err := os.Stat(s); err == nil && info.Mode().IsRegular() {
			return os.ReadFile(s)
		}
	}
	return []byte(s), nil
}

// toDER 从 PEM 或裸 Base64 中取出 DER 字节，同时返回 PEM 类型（裸 Base64 时为空）。
func toDER(s string) ([]byte, string, error) {
	data, err := load(s)
	if err != nil {
		return nil, "", err
	}
	if block, _ := pem.Decode(data); block != nil {
		return block.Bytes, block.Type, nil
	}
	clean := strings.Join(strings.Fields(string(data)), "")
	der, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		return nil, "", fmt.Errorf("密钥既不是 PEM 也不是合法的 Base64: %w", err)
	}
	return der, "", nil
}

// ParsePrivateKey 解析 RSA 私钥，兼容 PKCS#8 与 PKCS#1。
func ParsePrivateKey(s string) (*rsa.PrivateKey, error) {
	der, _, err := toDER(s)
	if err != nil {
		return nil, err
	}
	if k, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		if rk, ok := k.(*rsa.PrivateKey); ok {
			return rk, nil
		}
		return nil, errors.New("私钥不是 RSA 类型")
	}
	if k, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return k, nil
	}
	return nil, errors.New("无法解析 RSA 私钥（支持 PKCS#8 / PKCS#1）")
}

// ParsePublicKey 解析 RSA 公钥，兼容 PKIX、PKCS#1 以及 X.509 证书。
func ParsePublicKey(s string) (*rsa.PublicKey, error) {
	der, typ, err := toDER(s)
	if err != nil {
		return nil, err
	}
	if typ == "CERTIFICATE" {
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, err
		}
		if pk, ok := cert.PublicKey.(*rsa.PublicKey); ok {
			return pk, nil
		}
		return nil, errors.New("证书公钥不是 RSA 类型")
	}
	if k, err := x509.ParsePKIXPublicKey(der); err == nil {
		if pk, ok := k.(*rsa.PublicKey); ok {
			return pk, nil
		}
		return nil, errors.New("公钥不是 RSA 类型")
	}
	if k, err := x509.ParsePKCS1PublicKey(der); err == nil {
		return k, nil
	}
	return nil, errors.New("无法解析 RSA 公钥")
}

// Sign 使用 SHA256withRSA 签名，返回 Base64 结果。
func Sign(key *rsa.PrivateKey, data []byte) (string, error) {
	h := sha256.Sum256(data)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// Verify 校验 SHA256withRSA 签名（Base64 编码）。
func Verify(key *rsa.PublicKey, data []byte, signature string) error {
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("签名不是合法的 Base64: %w", err)
	}
	h := sha256.Sum256(data)
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, h[:], sig); err != nil {
		return errors.New("签名校验失败")
	}
	return nil
}
