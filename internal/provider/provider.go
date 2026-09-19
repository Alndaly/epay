// Package provider 定义上游支付渠道（支付宝、微信、PayPal……）的统一抽象。
//
// 网关业务层只面向 Provider 接口编程；每个渠道是一个独立子包，
// 在 init() 中通过 Register 注册自己的驱动，新增渠道无需改动任何已有代码。
package provider

import (
	"context"
	"errors"
	"net/http"

	"epay/internal/model"
	"epay/internal/money"
)

// Device 买家终端类型，用于选择 PC 收银台 / 手机 H5 / 扫码等不同支付产品。
type Device string

const (
	DevicePC     Device = "pc"
	DeviceMobile Device = "mobile"
	DeviceWechat Device = "wechat" // 微信内置浏览器
	DeviceAlipay Device = "alipay" // 支付宝客户端内
)

// IsMobile 是否为移动端环境。
func (d Device) IsMobile() bool { return d != DevicePC && d != "" }

// PayRequest 向上游发起支付所需的信息。
type PayRequest struct {
	Order     *model.Order
	NotifyURL string // 上游异步通知地址（指向本网关）
	ReturnURL string // 买家付款后跳回的地址（指向本网关）
	CancelURL string // 买家取消支付时跳回的地址（部分渠道需要）
	Device    Device
	ClientIP  string
}

// PayResult 上游下单结果。
type PayResult struct {
	Kind        model.PayKind
	Content     string // 跳转 URL 或二维码内容
	UpstreamRef string // 上游预下单 ID，可为空
	Currency    string // 实际扣款币种（大写 ISO 4217）
	Amount      int64  // 实际扣款金额（最小货币单位）
}

// Payment 经过验签或主动查询得到的、可信的上游支付结果。
type Payment struct {
	TradeNo    string // 本网关订单号
	Paid       bool   // 是否已支付成功
	APITradeNo string // 上游交易号
	Buyer      string
	Currency   string
	Amount     int64
}

// RefundRequest 退款请求。
type RefundRequest struct {
	Order    *model.Order
	RefundNo string      // 退款单号，同一笔退款重试时保持不变
	Amount   money.Cents // 以订单币种计的退款金额
}

// Provider 上游支付渠道。
type Provider interface {
	// Pay 在上游创建支付，返回买家的支付方式（跳转或二维码）。
	// 对同一订单重复调用应当是安全的（上游以 TradeNo 作幂等键）。
	Pay(ctx context.Context, req *PayRequest) (*PayResult, error)

	// ParseNotify 校验并解析上游异步通知。
	// 返回 (nil, nil) 表示这是一条合法但与支付成功无关的通知（例如退款事件），应直接应答成功。
	ParseNotify(ctx context.Context, r *http.Request) (*Payment, error)

	// AckNotify 按渠道要求向上游应答通知处理结果；err 为 nil 表示处理成功。
	AckNotify(w http.ResponseWriter, err error)

	// Query 主动查询订单在上游的支付状态，用于回调丢失时的兜底以及同步跳转时的确认。
	Query(ctx context.Context, o *model.Order) (*Payment, error)
}

// Refunder 支持退款的渠道实现此接口。
type Refunder interface {
	Refund(ctx context.Context, req *RefundRequest) error
}

// Simulator 仅用于测试的模拟渠道实现此接口，收银台据此展示"模拟支付"按钮。
type Simulator interface {
	Simulated() bool
}

// ErrNotSupported 渠道不支持某项操作。
var ErrNotSupported = errors.New("该支付渠道不支持此操作")
