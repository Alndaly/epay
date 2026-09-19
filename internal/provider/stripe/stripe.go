// Package stripe 实现 Stripe Checkout 渠道（信用卡、Apple Pay、Google Pay 等）。
//
// 流程：创建 Checkout Session → 买家跳转 Stripe 托管收银台 → 支付完成后
// 通过 Webhook（checkout.session.completed）与跳回时的主动查询双重确认。
package stripe

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"epay/internal/model"
	"epay/internal/provider"
)

const apiBase = "https://api.stripe.com"

// webhookTolerance Webhook 签名时间戳的最大容忍偏差（与官方 SDK 一致）。
const webhookTolerance = 5 * time.Minute

// Config Stripe 渠道配置。
type Config struct {
	SecretKey          string   `yaml:"secret_key"`           // sk_live_xxx / sk_test_xxx
	WebhookSecret      string   `yaml:"webhook_secret"`       // whsec_xxx
	PaymentMethodTypes []string `yaml:"payment_method_types"` // 为空时使用 Stripe 后台的动态支付方式配置
	provider.Exchange  `yaml:",inline"`
}

type Stripe struct {
	cfg  Config
	http *http.Client
}

func init() {
	provider.Register(provider.Driver{
		Name:        "stripe",
		Title:       "Stripe",
		Description: "Stripe Checkout，支持银行卡、Apple Pay、Google Pay 等",
		DefaultType: "stripe",
		Webhook:     "在 Stripe Dashboard → Developers → Webhooks 添加端点，订阅 checkout.session.completed 与 checkout.session.async_payment_succeeded 事件",
		Fields: append([]provider.Field{
			{Key: "secret_key", Label: "Secret Key", Type: provider.FieldText, Required: true, Secret: true, Placeholder: "sk_live_..."},
			{Key: "webhook_secret", Label: "Webhook 签名密钥", Type: provider.FieldText, Required: true, Secret: true, Placeholder: "whsec_..."},
			{Key: "payment_method_types", Label: "支付方式", Type: provider.FieldTags, Placeholder: "card, alipay, wechat_pay",
				Help: "留空则使用 Stripe 后台的支付方式设置"},
		}, provider.ExchangeFields("USD")...),
		New: func(opts provider.Options) (provider.Provider, error) {
			var cfg Config
			if err := opts.Decode(&cfg); err != nil {
				return nil, err
			}
			return New(cfg)
		},
	})
}

// New 创建 Stripe 渠道。
func New(cfg Config) (*Stripe, error) {
	if cfg.SecretKey == "" || cfg.WebhookSecret == "" {
		return nil, errors.New("stripe: secret_key / webhook_secret 不能为空")
	}
	if err := cfg.Exchange.Validate("USD"); err != nil {
		return nil, fmt.Errorf("stripe: %w", err)
	}
	return &Stripe{cfg: cfg, http: provider.NewHTTPClient()}, nil
}

func (s *Stripe) Pay(ctx context.Context, req *provider.PayRequest) (*provider.PayResult, error) {
	o := req.Order
	amount := s.cfg.Convert(o.Money)
	if amount <= 0 {
		return nil, errors.New("换算后的金额过小")
	}
	// Stripe 要求会话有效期在 30 分钟到 24 小时之间。
	expires := o.ExpireAt
	if minExp := time.Now().Add(31 * time.Minute); expires.Before(minExp) {
		expires = minExp
	}

	form := url.Values{}
	form.Set("mode", "payment")
	form.Set("success_url", req.ReturnURL)
	form.Set("cancel_url", req.CancelURL)
	form.Set("client_reference_id", o.TradeNo)
	form.Set("metadata[trade_no]", o.TradeNo)
	form.Set("payment_intent_data[metadata][trade_no]", o.TradeNo)
	form.Set("expires_at", strconv.FormatInt(expires.Unix(), 10))
	form.Set("line_items[0][quantity]", "1")
	form.Set("line_items[0][price_data][currency]", strings.ToLower(s.cfg.Currency))
	form.Set("line_items[0][price_data][unit_amount]", strconv.FormatInt(amount, 10))
	form.Set("line_items[0][price_data][product_data][name]", provider.Truncate(o.Name, 250))
	for i, t := range s.cfg.PaymentMethodTypes {
		form.Set(fmt.Sprintf("payment_method_types[%d]", i), t)
		if t == "wechat_pay" {
			form.Set("payment_method_options[wechat_pay][client]", "web")
		}
	}

	var sess session
	// Idempotency-Key 保证同一订单重复下单返回同一会话。
	if err := s.do(ctx, http.MethodPost, "/v1/checkout/sessions", o.TradeNo, form, &sess); err != nil {
		return nil, err
	}
	return &provider.PayResult{
		Kind:        model.PayKindRedirect,
		Content:     sess.URL,
		UpstreamRef: sess.ID,
		Currency:    s.cfg.Currency,
		Amount:      amount,
	}, nil
}

func (s *Stripe) ParseNotify(_ context.Context, r *http.Request) (*provider.Payment, error) {
	body, err := provider.ReadBody(r.Body)
	if err != nil {
		return nil, err
	}
	if err := verifySignature(r.Header.Get("Stripe-Signature"), body, s.cfg.WebhookSecret, time.Now()); err != nil {
		return nil, err
	}
	var event struct {
		Type string `json:"type"`
		Data struct {
			Object json.RawMessage `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, fmt.Errorf("stripe webhook 格式错误: %w", err)
	}
	switch event.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded":
		var sess session
		if err := json.Unmarshal(event.Data.Object, &sess); err != nil {
			return nil, err
		}
		return sess.payment(), nil
	default:
		return nil, nil
	}
}

func (s *Stripe) AckNotify(w http.ResponseWriter, err error) {
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Stripe) Query(ctx context.Context, o *model.Order) (*provider.Payment, error) {
	if o.UpstreamRef == "" {
		return &provider.Payment{TradeNo: o.TradeNo}, nil
	}
	var sess session
	if err := s.do(ctx, http.MethodGet, "/v1/checkout/sessions/"+url.PathEscape(o.UpstreamRef), "", nil, &sess); err != nil {
		return nil, err
	}
	return sess.payment(), nil
}

func (s *Stripe) Refund(ctx context.Context, req *provider.RefundRequest) error {
	if req.Order.APITradeNo == "" {
		return errors.New("缺少 Stripe PaymentIntent，无法退款")
	}
	form := url.Values{}
	form.Set("payment_intent", req.Order.APITradeNo)
	form.Set("amount", strconv.FormatInt(req.Order.PayAmountFor(req.Amount), 10))
	return s.do(ctx, http.MethodPost, "/v1/refunds", req.RefundNo, form, nil)
}

// session Stripe Checkout Session 中网关关心的字段。
type session struct {
	ID                string `json:"id"`
	URL               string `json:"url"`
	ClientReferenceID string `json:"client_reference_id"`
	PaymentStatus     string `json:"payment_status"` // paid | unpaid | no_payment_required
	PaymentIntent     string `json:"payment_intent"`
	AmountTotal       int64  `json:"amount_total"`
	Currency          string `json:"currency"`
	CustomerDetails   struct {
		Email string `json:"email"`
	} `json:"customer_details"`
}

func (s *session) payment() *provider.Payment {
	return &provider.Payment{
		TradeNo:    s.ClientReferenceID,
		Paid:       s.PaymentStatus == "paid",
		APITradeNo: s.PaymentIntent,
		Buyer:      s.CustomerDetails.Email,
		Currency:   strings.ToUpper(s.Currency),
		Amount:     s.AmountTotal,
	}
}

// verifySignature 校验 Stripe-Signature 头：t=时间戳,v1=HMAC_SHA256(secret, "t.body")。
func verifySignature(header string, body []byte, secret string, now time.Time) error {
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(sigs) == 0 {
		return errors.New("stripe webhook 签名头格式错误")
	}
	if d := now.Sub(time.Unix(sec, 0)); d > webhookTolerance || d < -webhookTolerance {
		return errors.New("stripe webhook 签名已过期")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	expected := mac.Sum(nil)
	for _, sig := range sigs {
		if got, err := hex.DecodeString(sig); err == nil && hmac.Equal(got, expected) {
			return nil
		}
	}
	return errors.New("stripe webhook 签名校验失败")
}

// do 调用 Stripe API（表单编码请求，JSON 响应）。
func (s *Stripe) do(ctx context.Context, method, path, idempotencyKey string, form url.Values, out any) error {
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(s.cfg.SecretKey, "")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("请求 Stripe 失败: %w", err)
	}
	respBody, err := provider.ReadBody(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		json.Unmarshal(respBody, &e)
		return fmt.Errorf("Stripe 返回错误 (HTTP %d): %s", resp.StatusCode, e.Error.Message)
	}
	if out != nil {
		return json.Unmarshal(respBody, out)
	}
	return nil
}
