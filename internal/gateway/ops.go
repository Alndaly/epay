package gateway

import (
	"context"
)

// 供管理后台使用的运维操作。

// Renotify 重新向商户投递支付成功通知（重置重试计数并立即投递）。
func (s *Service) Renotify(ctx context.Context, tradeNo string) error {
	ok, err := s.store.ResetNotify(ctx, tradeNo)
	if err != nil {
		return err
	}
	if !ok {
		return errorf("订单不存在或未支付")
	}
	s.notifier.Kick()
	return nil
}

// ForceSync 忽略节流，立即向上游查询订单状态；已支付则入账并通知商户。
// 与 Sync 不同，查询失败时会返回错误，便于管理员排查渠道问题。
func (s *Service) ForceSync(ctx context.Context, tradeNo string) error {
	o, err := s.Order(ctx, tradeNo)
	if err != nil {
		return err
	}
	if o.Paid() {
		return nil
	}
	if !o.HasPayment() {
		return errorf("订单未在上游下单成功，无法查询")
	}
	if _, err := s.query(ctx, o); err != nil {
		if IsPublic(err) {
			return err
		}
		return errorf("查询上游失败：%s", err)
	}
	return nil
}
