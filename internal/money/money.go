// Package money 提供以"分"为最小单位的金额类型。
//
// 支付系统中绝不能用浮点数表示金额，所有金额在系统内部统一以 int64 的"分"存储与计算，
// 只在协议边界（解析请求 / 输出响应）与十进制字符串互相转换。
package money

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Cents 表示以"分"（最小货币单位的 1/100 主单位）计的金额。
type Cents int64

// maxIntegerDigits 限制整数部分位数，防止溢出与异常输入。
const maxIntegerDigits = 12

var ErrInvalid = errors.New("金额格式不正确")

// Parse 将十进制金额字符串解析为分，例如 "10" -> 1000、"0.5" -> 50、"12.34" -> 1234。
// 不接受负数、科学计数法以及超过两位的小数。
func Parse(s string) (Cents, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, ErrInvalid
	}
	intPart, fracPart, hasDot := strings.Cut(s, ".")
	if intPart == "" || len(intPart) > maxIntegerDigits || !isDigits(intPart) {
		return 0, ErrInvalid
	}
	if hasDot && (fracPart == "" || len(fracPart) > 2 || !isDigits(fracPart)) {
		return 0, ErrInvalid
	}
	for len(fracPart) < 2 {
		fracPart += "0"
	}
	i, _ := strconv.ParseInt(intPart, 10, 64)
	f, _ := strconv.ParseInt(fracPart, 10, 64)
	return Cents(i*100 + f), nil
}

// String 以两位小数输出，例如 1234 -> "12.34"。
func (c Cents) String() string {
	sign := ""
	v := int64(c)
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}

// Convert 按汇率换算为另一币种的最小单位（四舍五入），用于 PayPal / Stripe 等外币通道。
// rate 表示 1 单位原币种可兑换的目标币种数量。
func (c Cents) Convert(rate float64) Cents {
	return Cents(math.Round(float64(c) * rate))
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
