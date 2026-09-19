package gateway

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"epay/internal/model"
	"epay/internal/store"
)

// retrySchedule 通知失败后的重试间隔（第 1 次投递立即进行），总计 12 次、跨度约 25 小时。
var retrySchedule = []time.Duration{
	15 * time.Second, 15 * time.Second, 30 * time.Second, 3 * time.Minute,
	10 * time.Minute, 20 * time.Minute, 30 * time.Minute, time.Hour,
	2 * time.Hour, 6 * time.Hour, 15 * time.Hour,
}

const (
	notifyBatchSize   = 50
	notifyConcurrency = 8
	notifyPollPeriod  = 5 * time.Second
)

// Notifier 负责向商户 notify_url 投递支付成功通知。
//
// 通知队列就是订单表本身（notify_status + next_notify_at），因此进程重启不会丢失待发通知；
// 订单支付成功后调用 Kick 可立即触发投递，无需等待下一轮轮询。
type Notifier struct {
	store  store.OrderStore
	params func(*model.Order) (map[string]string, error) // 生成带签名的通知参数
	client *http.Client
	log    *slog.Logger
	kick   chan struct{}
}

func newNotifier(st store.OrderStore, params func(*model.Order) (map[string]string, error), timeout time.Duration, log *slog.Logger) *Notifier {
	return &Notifier{
		store:  st,
		params: params,
		client: &http.Client{Timeout: timeout},
		log:    log,
		kick:   make(chan struct{}, 1),
	}
}

// Kick 唤醒投递循环（非阻塞）。
func (n *Notifier) Kick() {
	select {
	case n.kick <- struct{}{}:
	default:
	}
}

// Run 运行投递循环，直到 ctx 被取消。
func (n *Notifier) Run(ctx context.Context) {
	ticker := time.NewTicker(notifyPollPeriod)
	defer ticker.Stop()
	for {
		n.dispatch(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-n.kick:
		}
	}
}

// dispatch 分批取出到期通知并发投递。
func (n *Notifier) dispatch(ctx context.Context) {
	for ctx.Err() == nil {
		orders, err := n.store.DueNotifications(ctx, time.Now(), notifyBatchSize)
		if err != nil {
			n.log.Error("查询待通知订单失败", "err", err)
			return
		}
		var wg sync.WaitGroup
		sem := make(chan struct{}, notifyConcurrency)
		for _, o := range orders {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer func() { <-sem; wg.Done() }()
				n.deliver(ctx, o)
			}()
		}
		wg.Wait()
		if len(orders) < notifyBatchSize {
			return
		}
	}
}

// deliver 投递一次通知并记录结果与下次重试时间。
func (n *Notifier) deliver(ctx context.Context, o *model.Order) {
	err := n.send(ctx, o)
	u := store.NotifyUpdate{Count: o.NotifyCount + 1}
	switch {
	case err == nil:
		u.Status = model.NotifySuccess
		n.log.Info("商户通知成功", "trade_no", o.TradeNo, "attempt", u.Count)
	case u.Count > len(retrySchedule):
		u.Status, u.Error = model.NotifyFailed, err.Error()
		n.log.Error("商户通知多次失败，已放弃", "trade_no", o.TradeNo, "err", err)
	default:
		u.Status, u.Error = model.NotifyPending, err.Error()
		u.NextAt = time.Now().Add(retrySchedule[u.Count-1])
		n.log.Warn("商户通知失败，稍后重试", "trade_no", o.TradeNo, "attempt", u.Count, "next", u.NextAt, "err", err)
	}
	if err := n.store.UpdateNotify(ctx, o.TradeNo, u); err != nil {
		n.log.Error("保存通知状态失败", "trade_no", o.TradeNo, "err", err)
	}
}

// send 以 GET 方式请求商户 notify_url，商户返回 success 即视为成功（易支付协议约定）。
func (n *Notifier) send(ctx context.Context, o *model.Order) error {
	params, err := n.params(o)
	if err != nil {
		return err
	}
	target, err := url.Parse(o.NotifyURL)
	if err != nil {
		return fmt.Errorf("notify_url 无效: %w", err)
	}
	q := target.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	target.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "epay-gateway-notify/1.0")
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if text := strings.TrimSpace(string(body)); !strings.EqualFold(text, "success") {
		return fmt.Errorf("商户响应 HTTP %d: %q", resp.StatusCode, truncate(text, 100))
	}
	return nil
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}
