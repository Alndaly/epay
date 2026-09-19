// Package gateway 是网关的核心业务层：下单、确认支付、商户通知、查询与退款。
//
// 它只依赖 provider（上游渠道抽象）与 store（订单存储）两个接口，
// 与 HTTP 协议细节解耦，便于测试与复用。
package gateway

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"epay/internal/epay"
	"epay/internal/model"
	"epay/internal/money"
	"epay/internal/provider"
	"epay/internal/store"
)

// Options 网关配置。
type Options struct {
	BaseURL       string        // 网关对外访问地址，用于生成上游回调与跳转地址
	OrderTTL      time.Duration // 订单有效期
	NotifyTimeout time.Duration // 单次商户通知超时
	Logger        *slog.Logger
}

// syncInterval 主动查询上游的最小间隔，防止收银台轮询打爆上游接口。
const syncInterval = 3 * time.Second

type Service struct {
	store    store.Store
	baseURL  string
	orderTTL time.Duration
	notifier *Notifier
	log      *slog.Logger

	current  atomic.Pointer[snapshot] // 当前生效的商户与渠道配置，见 Reload
	reloadMu sync.Mutex

	lastSync  sync.Map     // trade_no -> time.Time，主动查询节流
	lastPrune atomic.Int64 // 上次清理 lastSync 的时间（Unix 秒）
}

// New 创建网关服务并从数据库加载商户与渠道配置。
func New(ctx context.Context, st store.Store, opts Options) (*Service, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.OrderTTL <= 0 {
		opts.OrderTTL = 30 * time.Minute
	}
	if opts.NotifyTimeout <= 0 {
		opts.NotifyTimeout = 10 * time.Second
	}
	s := &Service{
		store:    st,
		baseURL:  strings.TrimRight(opts.BaseURL, "/"),
		orderTTL: opts.OrderTTL,
		log:      opts.Logger,
	}
	s.current.Store(&snapshot{byID: map[int64]*Channel{}})
	s.notifier = newNotifier(st, s.merchantParams, opts.NotifyTimeout, opts.Logger)
	if err := s.Reload(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// RunNotifier 启动商户通知投递循环（阻塞直到 ctx 取消）。
func (s *Service) RunNotifier(ctx context.Context) { s.notifier.Run(ctx) }

// BaseURL 网关对外访问地址（不含末尾斜杠）。
func (s *Service) BaseURL() string { return s.baseURL }

// ---------------------------------------------------------------------------
// 下单
// ---------------------------------------------------------------------------

// CreateRequest 易支付下单请求（submit.php / mapi.php）。
type CreateRequest struct {
	Params    map[string]string // 原始请求参数（含 sign）
	ClientIP  string
	UserAgent string // 买家浏览器 UA；服务端下单（mapi.php）时为空
}

// CreateOrder 校验商户签名后创建订单，并在上游下单。
// 同一商户订单号重复提交时返回原订单（幂等），金额或支付方式不一致则报错。
func (s *Service) CreateOrder(ctx context.Context, req CreateRequest) (*model.Order, error) {
	p := req.Params
	m, ok := s.merchant(p["pid"])
	if !ok || !m.Enabled {
		return nil, errorf("商户不存在或已停用")
	}
	if !epay.Verify(p, m.Key) {
		return nil, errorf("签名校验失败")
	}

	o, err := s.buildOrder(p, req.ClientIP)
	if err != nil {
		return nil, err
	}
	ch, ok := s.Channel(o.Type)
	if !ok || !ch.Enabled {
		return nil, errorf("不支持的支付方式：%s", o.Type)
	}

	o, err = s.insertOrReuse(ctx, o)
	if err != nil {
		return nil, err
	}
	if err := s.ensurePayment(ctx, ch, o, DetectDevice(p["device"], req.UserAgent)); err != nil {
		return nil, err
	}
	return o, nil
}

// buildOrder 校验参数并构造新订单（尚未落库）。
func (s *Service) buildOrder(p map[string]string, clientIP string) (*model.Order, error) {
	for _, k := range []string{"type", "out_trade_no", "notify_url", "name", "money"} {
		if strings.TrimSpace(p[k]) == "" {
			return nil, errorf("缺少参数 %s", k)
		}
	}
	if len(p["out_trade_no"]) > 64 {
		return nil, errorf("out_trade_no 过长")
	}
	amount, err := money.Parse(p["money"])
	if err != nil || amount <= 0 {
		return nil, errorf("金额不正确：%s", p["money"])
	}
	if !isHTTPURL(p["notify_url"]) {
		return nil, errorf("notify_url 必须是 http(s) 地址")
	}
	if p["return_url"] != "" && !isHTTPURL(p["return_url"]) {
		return nil, errorf("return_url 必须是 http(s) 地址")
	}
	if clientIP == "" {
		clientIP = p["clientip"]
	}
	now := time.Now()
	return &model.Order{
		TradeNo:    newTradeNo(now),
		OutTradeNo: p["out_trade_no"],
		PID:        p["pid"],
		Type:       p["type"],
		Name:       truncate(p["name"], 127),
		Money:      amount,
		Param:      p["param"],
		NotifyURL:  p["notify_url"],
		ReturnURL:  p["return_url"],
		ClientIP:   clientIP,
		Device:     p["device"],
		CreatedAt:  now,
		ExpireAt:   now.Add(s.orderTTL),
	}, nil
}

// insertOrReuse 插入新订单；商户订单号已存在时按幂等规则复用或拒绝。
func (s *Service) insertOrReuse(ctx context.Context, o *model.Order) (*model.Order, error) {
	for attempt := 0; attempt < 3; attempt++ {
		err := s.store.Create(ctx, o)
		if err == nil {
			return o, nil
		}
		if !errors.Is(err, store.ErrDuplicate) {
			return nil, err
		}
		existing, err := s.store.GetByOutTradeNo(ctx, o.PID, o.OutTradeNo)
		if errors.Is(err, store.ErrNotFound) {
			o.TradeNo = newTradeNo(time.Now()) // 平台订单号碰撞（极小概率），换号重试
			continue
		}
		if err != nil {
			return nil, err
		}
		switch {
		case existing.Paid():
			return nil, errorf("该订单已支付")
		case existing.Money != o.Money || existing.Type != o.Type:
			return nil, errorf("商户订单号已存在且金额或支付方式不一致")
		case existing.Expired(time.Now()):
			return nil, errorf("订单已过期，请重新下单")
		}
		return existing, nil
	}
	return nil, errors.New("生成订单号失败")
}

// ensurePayment 若订单尚未在上游下单，则调用渠道下单并保存结果。
func (s *Service) ensurePayment(ctx context.Context, ch *Channel, o *model.Order, device provider.Device) error {
	if o.HasPayment() {
		return nil
	}
	res, err := ch.Provider.Pay(ctx, &provider.PayRequest{
		Order:     o,
		NotifyURL: s.baseURL + "/notify/" + url.PathEscape(ch.Type),
		ReturnURL: s.baseURL + "/return/" + o.TradeNo,
		CancelURL: s.baseURL + "/pay/" + o.TradeNo,
		Device:    device,
		ClientIP:  o.ClientIP,
	})
	if err != nil {
		s.log.Error("上游下单失败", "trade_no", o.TradeNo, "type", ch.Type, "err", err)
		return errorf("支付渠道下单失败，请稍后重试")
	}
	info := store.PaymentInfo{
		Kind:        res.Kind,
		Content:     res.Content,
		UpstreamRef: res.UpstreamRef,
		Currency:    strings.ToUpper(res.Currency),
		Amount:      res.Amount,
	}
	if err := s.store.SavePayment(ctx, o.TradeNo, info); err != nil {
		return err
	}
	o.PayKind, o.PayContent, o.UpstreamRef = info.Kind, info.Content, info.UpstreamRef
	o.PayCurrency, o.PayAmount = info.Currency, info.Amount
	return nil
}

// ---------------------------------------------------------------------------
// 支付确认
// ---------------------------------------------------------------------------

// HandleNotify 处理上游异步通知：验签解析 → 确认支付。返回 nil 表示应向上游应答成功。
func (s *Service) HandleNotify(ctx context.Context, ch *Channel, r *http.Request) error {
	pay, err := ch.Provider.ParseNotify(ctx, r)
	if err != nil {
		s.log.Warn("上游通知校验失败", "type", ch.Type, "err", err)
		return err
	}
	if pay == nil {
		return nil
	}
	return s.confirm(ctx, pay)
}

// Sync 主动向上游查询待支付订单的状态（带节流），返回最新订单。
// 用于买家跳回、收银台轮询等场景，作为异步通知丢失或延迟时的兜底；查询失败时静默返回原订单。
func (s *Service) Sync(ctx context.Context, o *model.Order) (*model.Order, error) {
	if o.Paid() || !o.HasPayment() {
		return o, nil
	}
	now := time.Now()
	s.pruneSync(now)
	if last, ok := s.lastSync.Load(o.TradeNo); ok && now.Sub(last.(time.Time)) < syncInterval {
		return o, nil
	}
	s.lastSync.Store(o.TradeNo, now)
	updated, err := s.query(ctx, o)
	if err != nil && !IsPublic(err) {
		s.log.Warn("主动查询上游失败", "trade_no", o.TradeNo, "err", err)
		return o, nil // 查询失败不影响展示，等待异步通知
	}
	return updated, err
}

// query 向上游查询订单，已支付则入账。
func (s *Service) query(ctx context.Context, o *model.Order) (*model.Order, error) {
	ch, ok := s.Channel(o.Type)
	if !ok {
		return o, errorf("支付方式 %s 不存在或初始化失败", o.Type)
	}
	pay, err := ch.Provider.Query(ctx, o)
	if err != nil {
		return o, err
	}
	if !pay.Paid {
		return o, nil
	}
	pay.TradeNo = o.TradeNo // 以本地订单为准，防止上游返回错单
	if err := s.confirm(ctx, pay); err != nil {
		return o, err
	}
	updated, err := s.store.GetByTradeNo(ctx, o.TradeNo)
	if err != nil {
		return o, err
	}
	return updated, nil
}

// pruneSync 每分钟最多一次，清理已超过订单有效期的节流记录，避免被放弃的订单占用内存。
func (s *Service) pruneSync(now time.Time) {
	last := s.lastPrune.Load()
	if now.Unix()-last < 60 || !s.lastPrune.CompareAndSwap(last, now.Unix()) {
		return
	}
	s.lastSync.Range(func(k, v any) bool {
		if now.Sub(v.(time.Time)) > s.orderTTL {
			s.lastSync.Delete(k)
		}
		return true
	})
}

// confirm 核对金额后将订单置为已支付并触发商户通知。重复调用是安全的。
func (s *Service) confirm(ctx context.Context, pay *provider.Payment) error {
	if !pay.Paid {
		return nil
	}
	o, err := s.store.GetByTradeNo(ctx, pay.TradeNo)
	if err != nil {
		return fmt.Errorf("查找订单 %s: %w", pay.TradeNo, err)
	}
	if o.Paid() {
		return nil
	}
	// 以下单时记录的上游币种与金额为准核对实付金额，防止篡改金额的回调。
	if !strings.EqualFold(pay.Currency, o.PayCurrency) || pay.Amount != o.PayAmount {
		s.log.Error("实付金额与订单不一致，拒绝入账", "trade_no", o.TradeNo,
			"expect", fmt.Sprintf("%d %s", o.PayAmount, o.PayCurrency),
			"got", fmt.Sprintf("%d %s", pay.Amount, pay.Currency))
		return errors.New("实付金额与订单不一致")
	}
	changed, err := s.store.MarkPaid(ctx, o.TradeNo, pay.APITradeNo, pay.Buyer, time.Now())
	if err != nil {
		return err
	}
	if changed {
		s.lastSync.Delete(o.TradeNo)
		s.log.Info("订单支付成功", "trade_no", o.TradeNo, "out_trade_no", o.OutTradeNo,
			"money", o.Money.String(), "type", o.Type, "api_trade_no", pay.APITradeNo)
		s.notifier.Kick()
	}
	return nil
}

// ---------------------------------------------------------------------------
// 查询 / 退款 / 回传参数
// ---------------------------------------------------------------------------

// Order 按平台订单号获取订单（收银台、跳转等内部使用）。
func (s *Service) Order(ctx context.Context, tradeNo string) (*model.Order, error) {
	o, err := s.store.GetByTradeNo(ctx, tradeNo)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errorf("订单不存在")
	}
	return o, err
}

// AuthMerchant 以明文密钥校验商户身份（易支付 api.php 的鉴权方式）。
func (s *Service) AuthMerchant(pid, key string) (model.Merchant, error) {
	m, ok := s.merchant(pid)
	if !ok || !m.Enabled || subtle.ConstantTimeCompare([]byte(m.Key), []byte(key)) != 1 {
		return model.Merchant{}, errorf("商户 ID 或密钥错误")
	}
	return m, nil
}

// MerchantOrder 查询商户自己的订单，tradeNo 与 outTradeNo 二选一。
func (s *Service) MerchantOrder(ctx context.Context, m model.Merchant, tradeNo, outTradeNo string) (*model.Order, error) {
	var (
		o   *model.Order
		err error
	)
	switch {
	case tradeNo != "":
		o, err = s.store.GetByTradeNo(ctx, tradeNo)
	case outTradeNo != "":
		o, err = s.store.GetByOutTradeNo(ctx, m.PID, outTradeNo)
	default:
		return nil, errorf("缺少参数 trade_no 或 out_trade_no")
	}
	if errors.Is(err, store.ErrNotFound) || (err == nil && o.PID != m.PID) {
		return nil, errorf("订单不存在")
	}
	if err != nil {
		return nil, err
	}
	return s.Sync(ctx, o)
}

// Refund 对已支付订单发起（部分）退款。
func (s *Service) Refund(ctx context.Context, m model.Merchant, tradeNo, outTradeNo, amountStr string) error {
	o, err := s.MerchantOrder(ctx, m, tradeNo, outTradeNo)
	if err != nil {
		return err
	}
	if !o.Paid() {
		return errorf("订单未支付，无法退款")
	}
	amount, err := money.Parse(amountStr)
	if err != nil || amount <= 0 {
		return errorf("退款金额不正确")
	}
	ch, ok := s.Channel(o.Type)
	if !ok {
		return errorf("支付方式 %s 不存在或初始化失败", o.Type)
	}
	refunder, ok := ch.Provider.(provider.Refunder)
	if !ok {
		return errorf("该支付方式不支持退款")
	}

	// 先在本地"占用"退款额度，防止并发请求导致超额退款；上游失败再回滚。
	reserved, err := s.store.ReserveRefund(ctx, o.TradeNo, amount)
	if err != nil {
		return err
	}
	if !reserved {
		return errorf("退款金额超过可退余额")
	}
	err = refunder.Refund(ctx, &provider.RefundRequest{Order: o, RefundNo: newTradeNo(time.Now()), Amount: amount})
	if err != nil {
		if rerr := s.store.ReleaseRefund(ctx, o.TradeNo, amount); rerr != nil {
			s.log.Error("回滚退款额度失败", "trade_no", o.TradeNo, "err", rerr)
		}
		s.log.Error("上游退款失败", "trade_no", o.TradeNo, "err", err)
		return errorf("退款失败：%s", err)
	}
	s.log.Info("退款成功", "trade_no", o.TradeNo, "amount", amount.String())
	return nil
}

// ReturnURL 生成跳回商户 return_url 的地址（携带与异步通知相同的签名参数）。
// 商户未提供 return_url 时返回空字符串。
func (s *Service) ReturnURL(o *model.Order) (string, error) {
	if o.ReturnURL == "" {
		return "", nil
	}
	params, err := s.merchantParams(o)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(o.ReturnURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// merchantParams 生成回传给商户的易支付标准参数（已签名）。
func (s *Service) merchantParams(o *model.Order) (map[string]string, error) {
	m, ok := s.merchant(o.PID)
	if !ok {
		return nil, fmt.Errorf("商户 %s 已不存在", o.PID)
	}
	return epay.SignParams(map[string]string{
		"pid":          o.PID,
		"trade_no":     o.TradeNo,
		"out_trade_no": o.OutTradeNo,
		"type":         o.Type,
		"name":         o.Name,
		"money":        o.Money.String(),
		"trade_status": epay.TradeSuccess,
		"param":        o.Param,
	}, m.Key), nil
}

// ---------------------------------------------------------------------------

// newTradeNo 生成 20 位平台订单号：yyyyMMddHHmmss + 6 位随机数。
// 满足微信（6-32 位）、支付宝（≤64 位）等各渠道对商户单号的要求。
func newTradeNo(t time.Time) string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1_000_000))
	return fmt.Sprintf("%s%06d", t.Format("20060102150405"), n.Int64())
}

func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
