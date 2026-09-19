// Package paypal 实现 PayPal Orders v2 渠道。
//
// 支付流程：
//  1. Pay：创建 PayPal 订单（intent=CAPTURE），把买家重定向到 PayPal 授权页；
//  2. 买家授权后跳回网关的 return 地址，网关调用 Query → 发现订单为 APPROVED → 执行 capture 扣款；
//  3. 同时订阅 Webhook 作为兜底：CHECKOUT.ORDER.APPROVED 时代为 capture，
//     PAYMENT.CAPTURE.COMPLETED 时确认到账（买家授权后直接关闭页面也不会漏单）。
//
// PayPal 不支持人民币结算，下单时按配置的汇率把订单金额换算为 currency。
package paypal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"epay/internal/model"
	"epay/internal/money"
	"epay/internal/provider"
)

const (
	apiProd    = "https://api-m.paypal.com"
	apiSandbox = "https://api-m.sandbox.paypal.com"
)

// Config PayPal 渠道配置。
type Config struct {
	ClientID          string `yaml:"client_id"`
	ClientSecret      string `yaml:"client_secret"`
	WebhookID         string `yaml:"webhook_id"` // 在开发者后台创建 Webhook 后获得，用于验签
	Sandbox           bool   `yaml:"sandbox"`
	BrandName         string `yaml:"brand_name"` // PayPal 页面展示的商家名称
	provider.Exchange `yaml:",inline"`
}

type PayPal struct {
	cfg  Config
	base string
	http *http.Client

	mu          sync.Mutex // 保护 access token 缓存
	token       string
	tokenExpiry time.Time
}

func init() {
	provider.Register(provider.Driver{
		Name:        "paypal",
		Title:       "PayPal",
		Description: "PayPal Orders v2，按汇率换算为外币扣款",
		DefaultType: "paypal",
		Webhook:     "在 PayPal 开发者后台的应用中添加 Webhook，订阅 Checkout order approved 与 Payment capture completed 事件",
		Fields: append([]provider.Field{
			{Key: "client_id", Label: "Client ID", Type: provider.FieldText, Required: true},
			{Key: "client_secret", Label: "Client Secret", Type: provider.FieldText, Required: true, Secret: true},
			{Key: "webhook_id", Label: "Webhook ID", Type: provider.FieldText, Required: true,
				Help: "创建 Webhook 后获得，用于校验回调签名"},
			{Key: "brand_name", Label: "商家名称", Type: provider.FieldText, Help: "PayPal 支付页展示的名称"},
			{Key: "sandbox", Label: "沙箱环境", Type: provider.FieldSwitch},
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

// New 创建 PayPal 渠道。
func New(cfg Config) (*PayPal, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("paypal: client_id / client_secret 不能为空")
	}
	if cfg.WebhookID == "" {
		return nil, errors.New("paypal: webhook_id 不能为空（用于校验 Webhook 签名）")
	}
	if err := cfg.Exchange.Validate("USD"); err != nil {
		return nil, fmt.Errorf("paypal: %w", err)
	}
	base := apiProd
	if cfg.Sandbox {
		base = apiSandbox
	}
	return &PayPal{cfg: cfg, base: base, http: provider.NewHTTPClient()}, nil
}

func (p *PayPal) Pay(ctx context.Context, req *provider.PayRequest) (*provider.PayResult, error) {
	o := req.Order
	amount := p.cfg.Convert(o.Money)
	if amount <= 0 {
		return nil, errors.New("换算后的金额过小")
	}
	body := map[string]any{
		"intent": "CAPTURE",
		"purchase_units": []map[string]any{{
			"custom_id":   o.TradeNo, // 回调中据此找回本网关订单
			"invoice_id":  o.TradeNo,
			"description": provider.Truncate(o.Name, 127),
			"amount":      ppAmount{Currency: p.cfg.Currency, Value: money.Cents(amount).String()},
		}},
		"payment_source": map[string]any{
			"paypal": map[string]any{
				"experience_context": map[string]any{
					"brand_name":          provider.Truncate(p.cfg.BrandName, 127),
					"user_action":         "PAY_NOW",
					"shipping_preference": "NO_SHIPPING",
					"return_url":          req.ReturnURL,
					"cancel_url":          req.CancelURL,
				},
			},
		},
	}
	var order ppOrder
	// PayPal-Request-Id 保证同一订单重复下单时返回同一个 PayPal 订单。
	if err := p.do(ctx, http.MethodPost, "/v2/checkout/orders", o.TradeNo, body, &order); err != nil {
		return nil, err
	}
	approve := order.link("payer-action")
	if approve == "" {
		approve = order.link("approve")
	}
	if approve == "" {
		return nil, errors.New("PayPal 未返回支付链接")
	}
	return &provider.PayResult{
		Kind:        model.PayKindRedirect,
		Content:     approve,
		UpstreamRef: order.ID,
		Currency:    p.cfg.Currency,
		Amount:      amount,
	}, nil
}

func (p *PayPal) ParseNotify(ctx context.Context, r *http.Request) (*provider.Payment, error) {
	body, err := provider.ReadBody(r.Body)
	if err != nil {
		return nil, err
	}
	if err := p.verifyWebhook(ctx, r.Header, body); err != nil {
		return nil, err
	}
	var event struct {
		EventType string          `json:"event_type"`
		Resource  json.RawMessage `json:"resource"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, fmt.Errorf("paypal webhook 格式错误: %w", err)
	}

	switch event.EventType {
	case "CHECKOUT.ORDER.APPROVED":
		// 买家已授权但尚未扣款，由网关代为 capture。
		var order ppOrder
		if err := json.Unmarshal(event.Resource, &order); err != nil {
			return nil, err
		}
		captured, err := p.capture(ctx, order.ID)
		if err != nil {
			return nil, err
		}
		return captured.payment(), nil
	case "PAYMENT.CAPTURE.COMPLETED":
		var c ppCapture
		if err := json.Unmarshal(event.Resource, &c); err != nil {
			return nil, err
		}
		return c.payment(""), nil
	default:
		return nil, nil
	}
}

func (p *PayPal) AckNotify(w http.ResponseWriter, err error) {
	// 非 2xx 时 PayPal 会重试投递 Webhook。
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Query 查询 PayPal 订单；若买家已授权（APPROVED）则立即 capture 完成扣款。
func (p *PayPal) Query(ctx context.Context, o *model.Order) (*provider.Payment, error) {
	if o.UpstreamRef == "" {
		return &provider.Payment{TradeNo: o.TradeNo}, nil
	}
	var order ppOrder
	if err := p.do(ctx, http.MethodGet, "/v2/checkout/orders/"+url.PathEscape(o.UpstreamRef), "", nil, &order); err != nil {
		return nil, err
	}
	if order.Status == "APPROVED" {
		captured, err := p.capture(ctx, order.ID)
		if err != nil {
			return nil, err
		}
		order = *captured
	}
	pay := order.payment()
	if pay.TradeNo == "" {
		pay.TradeNo = o.TradeNo
	}
	return pay, nil
}

func (p *PayPal) Refund(ctx context.Context, req *provider.RefundRequest) error {
	if req.Order.APITradeNo == "" {
		return errors.New("缺少 PayPal capture ID，无法退款")
	}
	body := map[string]any{
		"amount": ppAmount{
			Currency: req.Order.PayCurrency,
			Value:    money.Cents(req.Order.PayAmountFor(req.Amount)).String(),
		},
	}
	path := "/v2/payments/captures/" + url.PathEscape(req.Order.APITradeNo) + "/refund"
	return p.do(ctx, http.MethodPost, path, req.RefundNo, body, nil)
}

// capture 对已授权订单扣款；重复 capture 时 PayPal 会返回 ORDER_ALREADY_CAPTURED，此时回查订单即可。
func (p *PayPal) capture(ctx context.Context, orderID string) (*ppOrder, error) {
	var order ppOrder
	err := p.do(ctx, http.MethodPost, "/v2/checkout/orders/"+url.PathEscape(orderID)+"/capture", orderID+"-capture", struct{}{}, &order)
	var apiErr *apiError
	if errors.As(err, &apiErr) && strings.Contains(apiErr.body, "ORDER_ALREADY_CAPTURED") {
		err = p.do(ctx, http.MethodGet, "/v2/checkout/orders/"+url.PathEscape(orderID), "", nil, &order)
	}
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// verifyWebhook 调用 PayPal 官方接口校验 Webhook 签名。
func (p *PayPal) verifyWebhook(ctx context.Context, h http.Header, body []byte) error {
	req := map[string]any{
		"auth_algo":         h.Get("Paypal-Auth-Algo"),
		"cert_url":          h.Get("Paypal-Cert-Url"),
		"transmission_id":   h.Get("Paypal-Transmission-Id"),
		"transmission_sig":  h.Get("Paypal-Transmission-Sig"),
		"transmission_time": h.Get("Paypal-Transmission-Time"),
		"webhook_id":        p.cfg.WebhookID,
		"webhook_event":     json.RawMessage(body), // 必须原样提交事件内容
	}
	var resp struct {
		VerificationStatus string `json:"verification_status"`
	}
	if err := p.do(ctx, http.MethodPost, "/v1/notifications/verify-webhook-signature", "", req, &resp); err != nil {
		return fmt.Errorf("paypal webhook 验签请求失败: %w", err)
	}
	if resp.VerificationStatus != "SUCCESS" {
		return errors.New("paypal webhook 签名校验失败")
	}
	return nil
}

// ---- PayPal API 数据结构 ----

type ppAmount struct {
	Currency string `json:"currency_code"`
	Value    string `json:"value"`
}

type ppCapture struct {
	ID       string   `json:"id"`
	Status   string   `json:"status"`
	CustomID string   `json:"custom_id"`
	Amount   ppAmount `json:"amount"`
}

func (c *ppCapture) payment(buyer string) *provider.Payment {
	value, _ := money.Parse(c.Amount.Value)
	return &provider.Payment{
		TradeNo:    c.CustomID,
		Paid:       c.Status == "COMPLETED",
		APITradeNo: c.ID,
		Buyer:      buyer,
		Currency:   strings.ToUpper(c.Amount.Currency),
		Amount:     int64(value),
	}
}

type ppOrder struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	PurchaseUnits []struct {
		CustomID string `json:"custom_id"`
		Payments struct {
			Captures []ppCapture `json:"captures"`
		} `json:"payments"`
	} `json:"purchase_units"`
	Payer struct {
		Email string `json:"email_address"`
	} `json:"payer"`
	Links []struct {
		Href string `json:"href"`
		Rel  string `json:"rel"`
	} `json:"links"`
}

func (o *ppOrder) link(rel string) string {
	for _, l := range o.Links {
		if l.Rel == rel {
			return l.Href
		}
	}
	return ""
}

// payment 从订单中提取支付结果：只有存在 COMPLETED 的 capture 才算支付成功。
func (o *ppOrder) payment() *provider.Payment {
	pay := &provider.Payment{}
	if len(o.PurchaseUnits) == 0 {
		return pay
	}
	unit := o.PurchaseUnits[0]
	pay.TradeNo = unit.CustomID
	for _, c := range unit.Payments.Captures {
		if c.Status == "COMPLETED" {
			pay = c.payment(o.Payer.Email)
			if pay.TradeNo == "" {
				pay.TradeNo = unit.CustomID
			}
			break
		}
	}
	return pay
}

// ---- HTTP 调用 ----

type apiError struct {
	status int
	body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("PayPal 返回错误 (HTTP %d): %s", e.status, provider.Truncate(e.body, 512))
}

// do 调用 PayPal REST API。requestID 非空时作为 PayPal-Request-Id 实现幂等。
func (p *PayPal) do(ctx context.Context, method, path, requestID string, in, out any) error {
	token, err := p.accessToken(ctx)
	if err != nil {
		return err
	}
	var body []byte
	if in != nil {
		if body, err = json.Marshal(in); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, p.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Prefer", "return=representation")
	if requestID != "" {
		req.Header.Set("PayPal-Request-Id", requestID)
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("请求 PayPal 失败: %w", err)
	}
	respBody, err := provider.ReadBody(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return &apiError{status: resp.StatusCode, body: string(respBody)}
	}
	if out != nil && len(respBody) > 0 {
		return json.Unmarshal(respBody, out)
	}
	return nil
}

// accessToken 获取并缓存 OAuth2 access token（提前 1 分钟刷新）。
func (p *PayPal) accessToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.token != "" && time.Now().Before(p.tokenExpiry) {
		return p.token, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+"/v1/oauth2/token",
		strings.NewReader("grant_type=client_credentials"))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(p.cfg.ClientID, p.cfg.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("获取 PayPal token 失败: %w", err)
	}
	body, err := provider.ReadBody(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", &apiError{status: resp.StatusCode, body: string(body)}
	}
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &t); err != nil {
		return "", err
	}
	p.token = t.AccessToken
	p.tokenExpiry = time.Now().Add(time.Duration(t.ExpiresIn)*time.Second - time.Minute)
	return p.token, nil
}
