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

	Close() error
}
