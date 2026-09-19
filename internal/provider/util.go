package provider

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"epay/internal/money"
)

// 以下为各渠道共用的小工具。

// MaxBodySize 读取上游请求/响应体的上限，防止恶意超大报文。
const MaxBodySize = 1 << 20

// NewHTTPClient 返回渠道调用上游 API 使用的 HTTP 客户端。
func NewHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

// ReadBody 读取并关闭 body，超过 MaxBodySize 时报错。
func ReadBody(body io.ReadCloser) ([]byte, error) {
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, MaxBodySize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxBodySize {
		return nil, fmt.Errorf("报文超过 %d 字节", MaxBodySize)
	}
	return data, nil
}

// Truncate 按字节数截断字符串且不破坏 UTF-8 字符（上游对商品名普遍有字节长度限制）。
func Truncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	s = s[:maxBytes]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// Exchange 外币通道的币种与汇率配置。
type Exchange struct {
	Currency string  `yaml:"currency"`      // 上游扣款币种，如 USD
	Rate     float64 `yaml:"exchange_rate"` // 1 单位订单币种（人民币）= Rate 单位上游币种
}

// zeroDecimal 没有小数位的币种，本网关的金额模型（两位小数）不支持它们。
var zeroDecimal = map[string]bool{"JPY": true, "KRW": true, "TWD": true, "HUF": true, "VND": true, "CLP": true}

// Validate 校验并规范化外币配置。
func (e *Exchange) Validate(defaultCurrency string) error {
	e.Currency = strings.ToUpper(strings.TrimSpace(e.Currency))
	if e.Currency == "" {
		e.Currency = defaultCurrency
	}
	if zeroDecimal[e.Currency] {
		return fmt.Errorf("暂不支持无小数位币种 %s", e.Currency)
	}
	if e.Rate <= 0 {
		return fmt.Errorf("exchange_rate 必须大于 0（1 元人民币可兑换的 %s 数量）", e.Currency)
	}
	return nil
}

// Convert 将订单金额换算为上游金额（最小货币单位）。
func (e *Exchange) Convert(c money.Cents) int64 {
	return int64(c.Convert(e.Rate))
}
