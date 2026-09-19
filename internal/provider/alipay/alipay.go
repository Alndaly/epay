// Package alipay 实现支付宝开放平台渠道（公钥模式，RSA2 签名）。
//
// 支持三种支付产品，通过 mode 配置选择：
//   - page   电脑网站支付 alipay.trade.page.pay（跳转支付宝收银台）
//   - wap    手机网站支付 alipay.trade.wap.pay（跳转支付宝 App / H5）
//   - qrcode 当面付扫码 alipay.trade.precreate（在本网关收银台展示二维码）
//   - auto   移动端用 wap，PC 端用 page（默认）
package alipay

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"epay/internal/model"
	"epay/internal/money"
	"epay/internal/provider"
	"epay/internal/provider/keyutil"
)

const (
	gatewayProd    = "https://openapi.alipay.com/gateway.do"
	gatewaySandbox = "https://openapi-sandbox.dl.alipaydev.com/gateway.do"
)

// 支付宝接口时间均为北京时间。
var cst = time.FixedZone("CST", 8*3600)

// Config 支付宝渠道配置。
type Config struct {
	AppID           string `yaml:"app_id"`
	PrivateKey      string `yaml:"private_key"`       // 应用私钥（内容或文件路径）
	AlipayPublicKey string `yaml:"alipay_public_key"` // 支付宝公钥（注意不是应用公钥）
	Mode            string `yaml:"mode"`              // auto | page | wap | qrcode
	Sandbox         bool   `yaml:"sandbox"`
}

type Alipay struct {
	cfg     Config
	gateway string
	priv    *rsa.PrivateKey
	pub     *rsa.PublicKey
	http    *http.Client
}

func init() {
	provider.Register(provider.Driver{
		Name:        "alipay",
		Title:       "支付宝",
		Description: "支付宝开放平台（公钥模式 RSA2），支持电脑网站、手机网站与当面付扫码",
		DefaultType: "alipay",
		Fields: []provider.Field{
			{Key: "app_id", Label: "AppID", Type: provider.FieldText, Required: true, Placeholder: "2021000000000000"},
			{Key: "private_key", Label: "应用私钥", Type: provider.FieldTextarea, Required: true, Secret: true,
				Help: "开放平台密钥工具生成的应用私钥，支持 PEM 或裸 Base64，也可填写服务器上的文件路径"},
			{Key: "alipay_public_key", Label: "支付宝公钥", Type: provider.FieldTextarea, Required: true,
				Help: "开放平台「接口加签方式」中的支付宝公钥（注意不是应用公钥）"},
			{Key: "mode", Label: "支付产品", Type: provider.FieldSelect, Default: "auto", Options: []provider.FieldOption{
				{Value: "auto", Label: "自动（PC 电脑网站 / 手机网站）"},
				{Value: "page", Label: "电脑网站支付"},
				{Value: "wap", Label: "手机网站支付"},
				{Value: "qrcode", Label: "当面付扫码"},
			}, Help: "个人开发者通常只能开通当面付，请选择「当面付扫码」"},
			{Key: "sandbox", Label: "沙箱环境", Type: provider.FieldSwitch},
		},
		New: func(opts provider.Options) (provider.Provider, error) {
			var cfg Config
			if err := opts.Decode(&cfg); err != nil {
				return nil, err
			}
			return New(cfg)
		},
	})
}

// New 创建支付宝渠道。
func New(cfg Config) (*Alipay, error) {
	if cfg.AppID == "" {
		return nil, errors.New("alipay: app_id 不能为空")
	}
	switch cfg.Mode {
	case "":
		cfg.Mode = "auto"
	case "auto", "page", "wap", "qrcode":
	default:
		return nil, fmt.Errorf("alipay: 不支持的 mode %q", cfg.Mode)
	}
	priv, err := keyutil.ParsePrivateKey(cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("alipay: 应用私钥: %w", err)
	}
	pub, err := keyutil.ParsePublicKey(cfg.AlipayPublicKey)
	if err != nil {
		return nil, fmt.Errorf("alipay: 支付宝公钥: %w", err)
	}
	gw := gatewayProd
	if cfg.Sandbox {
		gw = gatewaySandbox
	}
	return &Alipay{cfg: cfg, gateway: gw, priv: priv, pub: pub, http: provider.NewHTTPClient()}, nil
}

func (a *Alipay) Pay(ctx context.Context, req *provider.PayRequest) (*provider.PayResult, error) {
	o := req.Order
	biz := map[string]any{
		"out_trade_no": o.TradeNo,
		"total_amount": o.Money.String(),
		"subject":      provider.Truncate(o.Name, 256),
	}
	if !o.ExpireAt.IsZero() {
		biz["time_expire"] = o.ExpireAt.In(cst).Format(time.DateTime)
	}
	result := &provider.PayResult{Currency: "CNY", Amount: int64(o.Money)}

	mode := a.cfg.Mode
	if mode == "auto" {
		mode = "page"
		if req.Device.IsMobile() {
			mode = "wap"
		}
	}

	switch mode {
	case "qrcode":
		var resp struct {
			respCommon
			QRCode string `json:"qr_code"`
		}
		if err := a.call(ctx, "alipay.trade.precreate", biz, req.NotifyURL, &resp); err != nil {
			return nil, err
		}
		result.Kind, result.Content = model.PayKindQRCode, resp.QRCode
	case "wap":
		biz["product_code"] = "QUICK_WAP_WAY"
		biz["quit_url"] = req.CancelURL
		result.Kind = model.PayKindRedirect
		result.Content = a.pageURL("alipay.trade.wap.pay", biz, req.NotifyURL, req.ReturnURL)
	default: // page
		biz["product_code"] = "FAST_INSTANT_TRADE_PAY"
		result.Kind = model.PayKindRedirect
		result.Content = a.pageURL("alipay.trade.page.pay", biz, req.NotifyURL, req.ReturnURL)
	}
	return result, nil
}

func (a *Alipay) ParseNotify(_ context.Context, r *http.Request) (*provider.Payment, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	params := map[string]string{}
	for k := range r.Form {
		params[k] = r.Form.Get(k)
	}
	if err := keyutil.Verify(a.pub, []byte(signContent(params, true)), params["sign"]); err != nil {
		return nil, fmt.Errorf("alipay 通知%w", err)
	}
	if params["app_id"] != a.cfg.AppID {
		return nil, fmt.Errorf("alipay 通知 app_id 不匹配: %s", params["app_id"])
	}
	amount, err := money.Parse(params["total_amount"])
	if err != nil {
		return nil, fmt.Errorf("alipay 通知金额无效: %q", params["total_amount"])
	}
	buyer := params["buyer_logon_id"]
	if buyer == "" {
		buyer = params["buyer_id"]
	}
	return &provider.Payment{
		TradeNo:    params["out_trade_no"],
		Paid:       isPaid(params["trade_status"]),
		APITradeNo: params["trade_no"],
		Buyer:      buyer,
		Currency:   "CNY",
		Amount:     int64(amount),
	}, nil
}

func (a *Alipay) AckNotify(w http.ResponseWriter, err error) {
	// 支付宝要求返回纯文本 success，否则会按策略重试（最长 24 小时）。
	if err != nil {
		w.Write([]byte("fail"))
		return
	}
	w.Write([]byte("success"))
}

func (a *Alipay) Query(ctx context.Context, o *model.Order) (*provider.Payment, error) {
	var resp struct {
		respCommon
		TradeNo      string `json:"trade_no"`
		OutTradeNo   string `json:"out_trade_no"`
		TradeStatus  string `json:"trade_status"`
		TotalAmount  string `json:"total_amount"`
		BuyerLogonID string `json:"buyer_logon_id"`
	}
	err := a.call(ctx, "alipay.trade.query", map[string]any{"out_trade_no": o.TradeNo}, "", &resp)
	if resp.SubCode == "ACQ.TRADE_NOT_EXIST" { // 买家尚未扫码/登录时交易不存在
		return &provider.Payment{TradeNo: o.TradeNo}, nil
	}
	if err != nil {
		return nil, err
	}
	amount, _ := money.Parse(resp.TotalAmount)
	return &provider.Payment{
		TradeNo:    resp.OutTradeNo,
		Paid:       isPaid(resp.TradeStatus),
		APITradeNo: resp.TradeNo,
		Buyer:      resp.BuyerLogonID,
		Currency:   "CNY",
		Amount:     int64(amount),
	}, nil
}

func (a *Alipay) Refund(ctx context.Context, req *provider.RefundRequest) error {
	var resp respCommon
	return a.call(ctx, "alipay.trade.refund", map[string]any{
		"out_trade_no":   req.Order.TradeNo,
		"refund_amount":  req.Amount.String(),
		"out_request_no": req.RefundNo,
	}, "", &resp)
}

func isPaid(status string) bool {
	return status == "TRADE_SUCCESS" || status == "TRADE_FINISHED"
}

// respCommon 支付宝接口的公共响应字段。
type respCommon struct {
	Code    string `json:"code"`
	Msg     string `json:"msg"`
	SubCode string `json:"sub_code"`
	SubMsg  string `json:"sub_msg"`
}

func (r respCommon) err() error {
	if r.Code == "10000" {
		return nil
	}
	if r.SubMsg != "" {
		return fmt.Errorf("支付宝返回错误: %s (%s)", r.SubMsg, r.SubCode)
	}
	return fmt.Errorf("支付宝返回错误: %s (%s)", r.Msg, r.Code)
}

// commonParams 构造公共请求参数并签名。
func (a *Alipay) commonParams(method string, biz any, notifyURL, returnURL string) url.Values {
	bizJSON, _ := json.Marshal(biz)
	v := url.Values{}
	v.Set("app_id", a.cfg.AppID)
	v.Set("method", method)
	v.Set("format", "JSON")
	v.Set("charset", "utf-8")
	v.Set("sign_type", "RSA2")
	v.Set("timestamp", time.Now().In(cst).Format(time.DateTime))
	v.Set("version", "1.0")
	v.Set("biz_content", string(bizJSON))
	if notifyURL != "" {
		v.Set("notify_url", notifyURL)
	}
	if returnURL != "" {
		v.Set("return_url", returnURL)
	}

	params := make(map[string]string, len(v))
	for k := range v {
		params[k] = v.Get(k)
	}
	// 私钥在 New 中已校验，签名不会失败。
	sig, _ := keyutil.Sign(a.priv, []byte(signContent(params, false)))
	v.Set("sign", sig)
	return v
}

// pageURL 生成页面跳转类接口（page.pay / wap.pay）的完整 GET 地址。
func (a *Alipay) pageURL(method string, biz any, notifyURL, returnURL string) string {
	return a.gateway + "?" + a.commonParams(method, biz, notifyURL, returnURL).Encode()
}

// call 调用支付宝服务端 API，校验响应签名后将业务节点解析到 out。
// out 必须内嵌 respCommon。
func (a *Alipay) call(ctx context.Context, method string, biz any, notifyURL string, out interface{ err() error }) error {
	form := a.commonParams(method, biz, notifyURL, "")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.gateway, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("请求支付宝失败: %w", err)
	}
	body, err := provider.ReadBody(resp.Body)
	if err != nil {
		return err
	}

	// 响应格式：{"<method>_response": {...}, "sign": "..."}，签名针对业务节点的原始 JSON 字节。
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("解析支付宝响应失败: %w", err)
	}
	node := strings.ReplaceAll(method, ".", "_") + "_response"
	raw, ok := envelope[node]
	if !ok {
		raw, ok = envelope["error_response"]
	}
	if !ok {
		return fmt.Errorf("支付宝响应缺少 %s 节点", node)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("解析支付宝响应失败: %w", err)
	}

	var sign string
	json.Unmarshal(envelope["sign"], &sign)
	if sign != "" {
		if err := keyutil.Verify(a.pub, raw, sign); err != nil {
			return fmt.Errorf("支付宝响应%w", err)
		}
	} else if out.err() == nil {
		// 成功响应必须带签名，否则可能是伪造的响应。
		return errors.New("支付宝响应缺少签名")
	}
	return out.err()
}

// signContent 生成待签名字符串：去掉 sign 与空值参数后按键名升序拼接为 k=v&k=v。
// 支付宝的规则：请求签名包含 sign_type，而异步通知验签时需要额外去掉 sign_type。
func signContent(params map[string]string, excludeSignType bool) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if v == "" || k == "sign" || (excludeSignType && k == "sign_type") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(k + "=" + params[k])
	}
	return b.String()
}
