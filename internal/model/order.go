// Package model 定义网关的核心领域对象。
package model

import (
	"time"

	"epay/internal/money"
)

// OrderStatus 订单支付状态，数值与易支付协议中的 status 字段一致。
type OrderStatus int

const (
	StatusPending OrderStatus = 0 // 待支付
	StatusPaid    OrderStatus = 1 // 已支付
)

// NotifyStatus 商户异步通知的投递状态。
type NotifyStatus int

const (
	NotifyNone    NotifyStatus = 0 // 订单未支付，无需通知
	NotifyPending NotifyStatus = 1 // 等待投递（含重试中）
	NotifySuccess NotifyStatus = 2 // 商户已返回 success
	NotifyFailed  NotifyStatus = 3 // 重试次数耗尽，放弃
)

// PayKind 描述买家应如何完成支付。
type PayKind string

const (
	PayKindRedirect PayKind = "redirect" // 跳转到上游收银台（Content 为 URL）
	PayKindQRCode   PayKind = "qrcode"   // 展示二维码扫码支付（Content 为二维码内容）
)

// Order 是网关内的一笔支付订单。
//
// 金额有两套：Money 为商户下单金额（易支付协议使用的币种，通常是人民币）；
// PayAmount/PayCurrency 为实际向上游发起支付的金额与币种（外币通道会按汇率换算），
// 回调验签后以后者校验实付金额。
type Order struct {
	TradeNo    string      // 平台订单号，本网关生成，也作为上游的商户单号
	OutTradeNo string      // 商户订单号
	PID        string      // 商户 ID
	Type       string      // 支付方式（易支付 type，如 alipay / wxpay / paypal）
	Name       string      // 商品名称
	Money      money.Cents // 订单金额
	Param      string      // 商户自定义参数，原样回传
	NotifyURL  string      // 商户异步通知地址
	ReturnURL  string      // 商户同步跳转地址
	ClientIP   string
	Device     string

	Status      OrderStatus
	PayKind     PayKind // 上游下单后得到的支付方式
	PayContent  string  // 跳转链接或二维码内容
	UpstreamRef string  // 上游预下单 ID（PayPal Order ID / Stripe Session ID），用于主动查询
	PayCurrency string  // 上游币种，如 CNY / USD
	PayAmount   int64   // 上游金额（最小货币单位）
	APITradeNo  string  // 上游交易号（支付宝 trade_no / 微信 transaction_id 等）
	Buyer       string  // 付款人标识
	RefundMoney money.Cents

	NotifyStatus NotifyStatus
	NotifyCount  int
	NextNotifyAt time.Time
	NotifyError  string

	CreatedAt time.Time
	ExpireAt  time.Time
	PaidAt    time.Time
}

// Paid 订单是否已支付。
func (o *Order) Paid() bool { return o.Status == StatusPaid }

// Expired 待支付订单是否已超过有效期。
func (o *Order) Expired(now time.Time) bool {
	return o.Status == StatusPending && !o.ExpireAt.IsZero() && now.After(o.ExpireAt)
}

// HasPayment 是否已在上游成功下单。
func (o *Order) HasPayment() bool { return o.PayContent != "" }

// PayAmountFor 将以订单币种计的金额按实付比例换算为上游金额，用于部分退款。
// 全额时直接返回 PayAmount，避免四舍五入误差。
func (o *Order) PayAmountFor(amount money.Cents) int64 {
	if amount >= o.Money || o.Money == 0 {
		return o.PayAmount
	}
	return (o.PayAmount*int64(amount) + int64(o.Money)/2) / int64(o.Money)
}
