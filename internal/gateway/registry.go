package gateway

import (
	"bytes"
	"context"
	"fmt"

	"epay/internal/model"
	"epay/internal/provider"
)

// Channel 运行中的支付方式：数据库中的配置 + 由其构造出的上游渠道实例。
type Channel struct {
	model.ChannelConfig
	Provider provider.Provider
}

// snapshot 某一时刻的商户与渠道配置。
//
// 配置变更时构造一份全新的 snapshot 并原子替换（copy-on-write），
// 处理中的请求继续使用旧快照，因此热更新无需加锁、也不会读到半更新的状态。
type snapshot struct {
	merchants map[string]model.Merchant // pid -> 商户
	channels  map[string]*Channel       // 易支付 type -> 渠道
	byID      map[int64]*Channel
	errors    map[int64]string // 初始化失败的渠道 ID -> 错误信息
}

func (s *Service) snap() *snapshot { return s.current.Load() }

// Reload 从数据库重新加载商户与渠道配置并原子生效。
//
// 单个渠道初始化失败（如密钥格式错误）不会影响其他渠道，错误信息可通过 ChannelError 查询。
// 配置未变化的渠道复用原实例，以保留其内部状态（如 PayPal 的 access token 缓存）。
func (s *Service) Reload(ctx context.Context) error {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	merchants, err := s.store.ListMerchants(ctx)
	if err != nil {
		return fmt.Errorf("加载商户: %w", err)
	}
	configs, err := s.store.ListChannels(ctx)
	if err != nil {
		return fmt.Errorf("加载支付渠道: %w", err)
	}

	old := s.snap()
	next := &snapshot{
		merchants: make(map[string]model.Merchant, len(merchants)),
		channels:  make(map[string]*Channel, len(configs)),
		byID:      make(map[int64]*Channel, len(configs)),
		errors:    make(map[int64]string),
	}
	for _, m := range merchants {
		next.merchants[m.PID] = m
	}
	for _, cfg := range configs {
		var p provider.Provider
		if prev, ok := old.byID[cfg.ID]; ok && prev.Driver == cfg.Driver && bytes.Equal(prev.Options, cfg.Options) {
			p = prev.Provider
		} else if p, err = provider.New(cfg.Driver, provider.JSONOptions(cfg.Options)); err != nil {
			next.errors[cfg.ID] = err.Error()
			s.log.Error("支付渠道初始化失败", "type", cfg.Type, "driver", cfg.Driver, "err", err)
			continue
		}
		ch := &Channel{ChannelConfig: cfg, Provider: p}
		next.channels[cfg.Type] = ch
		next.byID[cfg.ID] = ch
		if _, ok := p.(provider.Simulator); ok && cfg.Enabled {
			s.log.Warn("已启用模拟支付渠道，任何人都可以模拟支付成功，切勿用于生产环境", "type", cfg.Type)
		}
	}
	s.current.Store(next)
	s.log.Info("配置已加载", "merchants", len(next.merchants), "channels", len(next.channels), "failed", len(next.errors))
	return nil
}

// ChannelError 返回渠道初始化失败的原因；正常时返回空字符串。
func (s *Service) ChannelError(id int64) string {
	return s.snap().errors[id]
}

// Channel 按易支付 type 获取渠道（包括已停用的渠道，用于处理存量订单的回调）。
func (s *Service) Channel(typ string) (*Channel, bool) {
	ch, ok := s.snap().channels[typ]
	return ch, ok
}

// merchant 获取商户（包括已停用的商户，用于为存量订单签名通知）。
func (s *Service) merchant(pid string) (model.Merchant, bool) {
	m, ok := s.snap().merchants[pid]
	return m, ok
}
