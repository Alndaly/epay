// Package store 定义订单持久化接口。业务层只依赖该接口，
// 以便在 SQLite 之外按需扩展 MySQL / PostgreSQL 等实现。
package store

import (
	"context"
	"errors"
	"time"

	"epay/internal/model"
	"epay/internal/money"
)

var (
	ErrNotFound  = errors.New("订单不存在")
	ErrDuplicate = errors.New("订单已存在")
)

// PaymentInfo 上游下单成功后需要保存的信息。
type PaymentInfo struct {
	Kind        model.PayKind
	Content     string
	UpstreamRef string
	Currency    string
	Amount      int64
}

// NotifyUpdate 一次通知投递后的状态。
type NotifyUpdate struct {
	Status model.NotifyStatus
	Count  int
	NextAt time.Time
	Error  string
}

// OrderStore 订单存储。所有状态变更方法都必须是条件更新（CAS），
// 以保证在并发回调、重复回调时的幂等与正确性。
type OrderStore interface {
	// Create 新建订单；(pid, out_trade_no) 重复时返回 ErrDuplicate。
	Create(ctx context.Context, o *model.Order) error
	GetByTradeNo(ctx context.Context, tradeNo string) (*model.Order, error)
	GetByOutTradeNo(ctx context.Context, pid, outTradeNo string) (*model.Order, error)

	// SavePayment 保存上游下单结果（仅待支付订单）。
	SavePayment(ctx context.Context, tradeNo string, info PaymentInfo) error
	// MarkPaid 将待支付订单置为已支付并排入通知队列；返回是否由本次调用完成了状态变更。
	MarkPaid(ctx context.Context, tradeNo, apiTradeNo, buyer string, paidAt time.Time) (bool, error)

	// DueNotifications 返回到期需要投递通知的订单。
	DueNotifications(ctx context.Context, now time.Time, limit int) ([]*model.Order, error)
	UpdateNotify(ctx context.Context, tradeNo string, u NotifyUpdate) error

	// ReserveRefund 原子地累加已退款金额，超出订单金额时返回 false。
	ReserveRefund(ctx context.Context, tradeNo string, amount money.Cents) (bool, error)
	// ReleaseRefund 上游退款失败时回滚 ReserveRefund。
	ReleaseRefund(ctx context.Context, tradeNo string, amount money.Cents) error
}

// OrderFilter 管理后台的订单列表筛选条件。
type OrderFilter struct {
	Keyword string             // 模糊匹配平台订单号 / 商户订单号 / 上游交易号 / 商品名
	Status  *model.OrderStatus // 为空表示不限
	Type    string
	PID     string
	Notify  *model.NotifyStatus
	Offset  int
	Limit   int
}

// DailyStat 按天汇总的已支付订单。
type DailyStat struct {
	Date   string      `json:"date"` // 2006-01-02（服务器本地时区）
	Count  int         `json:"count"`
	Amount money.Cents `json:"amount"`
}

// Summary 管理后台概览数据。
type Summary struct {
	TodayOrders   int         `json:"todayOrders"`   // 今日下单数
	TodayPaid     int         `json:"todayPaid"`     // 今日支付成功数
	TodayAmount   money.Cents `json:"todayAmount"`   // 今日成交金额
	TotalAmount   money.Cents `json:"totalAmount"`   // 累计成交金额
	NotifyPending int         `json:"notifyPending"` // 待投递 / 重试中的商户通知
	NotifyFailed  int         `json:"notifyFailed"`  // 已放弃的商户通知
	Daily         []DailyStat `json:"daily"`         // 最近 N 天成交趋势（含无数据的日期）
}

// AdminStore 管理后台使用的查询与运维操作。
type AdminStore interface {
	ListOrders(ctx context.Context, f OrderFilter) ([]*model.Order, int, error)
	Summary(ctx context.Context, now time.Time, days int) (*Summary, error)
	// ResetNotify 重置通知状态并立即排队（用于手动补发通知）。仅对已支付订单生效。
	ResetNotify(ctx context.Context, tradeNo string) (bool, error)
}

// ConfigStore 商户、支付渠道与系统设置的持久化。
type ConfigStore interface {
	ListMerchants(ctx context.Context) ([]model.Merchant, error)
	// CreateMerchant pid 重复时返回 ErrDuplicate。
	CreateMerchant(ctx context.Context, m *model.Merchant) error
	UpdateMerchant(ctx context.Context, m *model.Merchant) error
	DeleteMerchant(ctx context.Context, pid string) error

	ListChannels(ctx context.Context) ([]model.ChannelConfig, error)
	GetChannel(ctx context.Context, id int64) (*model.ChannelConfig, error)
	// CreateChannel type 重复时返回 ErrDuplicate。
	CreateChannel(ctx context.Context, c *model.ChannelConfig) error
	UpdateChannel(ctx context.Context, c *model.ChannelConfig) error
	DeleteChannel(ctx context.Context, id int64) error

	// GetSetting 键不存在时返回空字符串。
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
}

// Store 完整的存储能力。
type Store interface {
	OrderStore
	AdminStore
	ConfigStore
	Close() error
}
