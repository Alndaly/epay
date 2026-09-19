// Package wechat 实现微信支付 APIv3 渠道。
//
// 验签采用"微信支付公钥"模式（2024 年后新商户的默认方式），无需下载平台证书。
// 支持的支付产品（mode）：
//   - native 扫码支付，在本网关收银台展示二维码（默认，所有商户均可开通）
//   - h5     手机浏览器跳转微信支付（需在商户平台单独申请 H5 权限）
//   - auto   移动端（非微信内）用 h5，其余用 native
package wechat

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"epay/internal/model"
	"epay/internal/provider"
	"epay/internal/provider/keyutil"
)

const apiBase = "https://api.mch.weixin.qq.com"

// 回调与响应的签名时间戳允许的最大偏差，用于防重放。
const maxClockSkew = 5 * time.Minute

// Config 微信支付渠道配置。
type Config struct {
	AppID       string `yaml:"app_id"`        // 公众号 / 小程序 / 移动应用 AppID
	MchID       string `yaml:"mch_id"`        // 商户号
	APIv3Key    string `yaml:"api_v3_key"`    // APIv3 密钥（32 字节）
	SerialNo    string `yaml:"serial_no"`     // 商户 API 证书序列号
	PrivateKey  string `yaml:"private_key"`   // 商户 API 私钥 apiclient_key.pem（内容或路径）
	PublicKeyID string `yaml:"public_key_id"` // 微信支付公钥 ID（PUB_KEY_ID_xxx）
	PublicKey   string `yaml:"public_key"`    // 微信支付公钥 pub_key.pem（内容或路径）
	Mode        string `yaml:"mode"`          // native | h5 | auto
}

type Wechat struct {
	cfg  Config
	priv *rsa.PrivateKey
	pub  *rsa.PublicKey
	http *http.Client
}

func init() {
	provider.Register(provider.Driver{
		Name:        "wechat",
		Title:       "微信支付",
		Description: "微信支付 APIv3（微信支付公钥验签），支持 Native 扫码与 H5",
		DefaultType: "wxpay",
		Fields: []provider.Field{
			{Key: "app_id", Label: "AppID", Type: provider.FieldText, Required: true, Placeholder: "wx0000000000000000",
				Help: "与商户号绑定的公众号 / 小程序 / 移动应用 AppID"},
			{Key: "mch_id", Label: "商户号", Type: provider.FieldText, Required: true, Placeholder: "1900000000"},
			{Key: "api_v3_key", Label: "APIv3 密钥", Type: provider.FieldText, Required: true, Secret: true,
				Help: "商户平台 → API 安全中设置的 32 位密钥"},
			{Key: "serial_no", Label: "商户证书序列号", Type: provider.FieldText, Required: true},
			{Key: "private_key", Label: "商户 API 私钥", Type: provider.FieldTextarea, Required: true, Secret: true,
				Help: "apiclient_key.pem 的内容或文件路径"},
			{Key: "public_key_id", Label: "微信支付公钥 ID", Type: provider.FieldText, Required: true, Placeholder: "PUB_KEY_ID_..."},
			{Key: "public_key", Label: "微信支付公钥", Type: provider.FieldTextarea, Required: true,
				Help: "pub_key.pem 的内容或文件路径"},
			{Key: "mode", Label: "支付产品", Type: provider.FieldSelect, Default: "native", Options: []provider.FieldOption{
				{Value: "native", Label: "Native 扫码"},
				{Value: "h5", Label: "H5 跳转（需单独开通）"},
				{Value: "auto", Label: "自动（手机 H5 / 其余扫码）"},
			}},
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

// New 创建微信支付渠道。
func New(cfg Config) (*Wechat, error) {
	switch {
	case cfg.AppID == "" || cfg.MchID == "" || cfg.SerialNo == "" || cfg.PublicKeyID == "":
		return nil, errors.New("wechat: app_id / mch_id / serial_no / public_key_id 均不能为空")
	case len(cfg.APIv3Key) != 32:
		return nil, errors.New("wechat: api_v3_key 必须为 32 个字符")
	}
	switch cfg.Mode {
	case "":
		cfg.Mode = "native"
	case "native", "h5", "auto":
	default:
		return nil, fmt.Errorf("wechat: 不支持的 mode %q", cfg.Mode)
	}
	priv, err := keyutil.ParsePrivateKey(cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("wechat: 商户私钥: %w", err)
	}
	pub, err := keyutil.ParsePublicKey(cfg.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("wechat: 微信支付公钥: %w", err)
	}
	return &Wechat{cfg: cfg, priv: priv, pub: pub, http: provider.NewHTTPClient()}, nil
}

func (w *Wechat) Pay(ctx context.Context, req *provider.PayRequest) (*provider.PayResult, error) {
	o := req.Order
	body := map[string]any{
		"appid":        w.cfg.AppID,
		"mchid":        w.cfg.MchID,
		"description":  provider.Truncate(o.Name, 127),
		"out_trade_no": o.TradeNo,
		"notify_url":   req.NotifyURL,
		"amount":       map[string]any{"total": int64(o.Money), "currency": "CNY"},
	}
	if !o.ExpireAt.IsZero() {
		body["time_expire"] = o.ExpireAt.Format(time.RFC3339)
	}
	result := &provider.PayResult{Currency: "CNY", Amount: int64(o.Money)}

	useH5 := w.cfg.Mode == "h5" ||
		(w.cfg.Mode == "auto" && req.Device.IsMobile() && req.Device != provider.DeviceWechat)
	if useH5 {
		body["scene_info"] = map[string]any{
			"payer_client_ip": req.ClientIP,
			"h5_info":         map[string]string{"type": "Wap"},
		}
		var resp struct {
			H5URL string `json:"h5_url"`
		}
		if err := w.do(ctx, http.MethodPost, "/v3/pay/transactions/h5", body, &resp); err != nil {
			return nil, err
		}
		// 支付完成后微信会跳转到 redirect_url。
		result.Kind = model.PayKindRedirect
		result.Content = resp.H5URL + "&redirect_url=" + url.QueryEscape(req.ReturnURL)
		return result, nil
	}

	var resp struct {
		CodeURL string `json:"code_url"`
	}
	if err := w.do(ctx, http.MethodPost, "/v3/pay/transactions/native", body, &resp); err != nil {
		return nil, err
	}
	result.Kind, result.Content = model.PayKindQRCode, resp.CodeURL
	return result, nil
}

// transaction 微信支付订单（查询结果与回调解密后的内容结构相同）。
type transaction struct {
	OutTradeNo    string `json:"out_trade_no"`
	TransactionID string `json:"transaction_id"`
	TradeState    string `json:"trade_state"`
	Amount        struct {
		Total    int64  `json:"total"`
		Currency string `json:"currency"`
	} `json:"amount"`
	Payer struct {
		OpenID string `json:"openid"`
	} `json:"payer"`
}

func (t *transaction) payment() *provider.Payment {
	return &provider.Payment{
		TradeNo:    t.OutTradeNo,
		Paid:       t.TradeState == "SUCCESS",
		APITradeNo: t.TransactionID,
		Buyer:      t.Payer.OpenID,
		Currency:   t.Amount.Currency,
		Amount:     t.Amount.Total,
	}
}

func (w *Wechat) ParseNotify(_ context.Context, r *http.Request) (*provider.Payment, error) {
	body, err := provider.ReadBody(r.Body)
	if err != nil {
		return nil, err
	}
	if err := w.verify(r.Header, body); err != nil {
		return nil, fmt.Errorf("wechat 通知%w", err)
	}

	var notice struct {
		EventType string `json:"event_type"`
		Resource  struct {
			Algorithm      string `json:"algorithm"`
			Ciphertext     string `json:"ciphertext"`
			AssociatedData string `json:"associated_data"`
			Nonce          string `json:"nonce"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(body, &notice); err != nil {
		return nil, fmt.Errorf("wechat 通知格式错误: %w", err)
	}
	if notice.EventType != "TRANSACTION.SUCCESS" {
		return nil, nil // 退款等其他事件，直接应答成功
	}
	plain, err := decryptAESGCM(w.cfg.APIv3Key, notice.Resource.Nonce, notice.Resource.AssociatedData, notice.Resource.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("wechat 通知解密失败: %w", err)
	}
	var t transaction
	if err := json.Unmarshal(plain, &t); err != nil {
		return nil, fmt.Errorf("wechat 通知内容错误: %w", err)
	}
	return t.payment(), nil
}

func (w *Wechat) AckNotify(rw http.ResponseWriter, err error) {
	// APIv3：成功应答 200/204 且无需应答体；失败应答 4xx/5xx，微信会按策略重试。
	if err != nil {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(rw).Encode(map[string]string{"code": "FAIL", "message": "失败"})
		return
	}
	rw.WriteHeader(http.StatusNoContent)
}

func (w *Wechat) Query(ctx context.Context, o *model.Order) (*provider.Payment, error) {
	var t transaction
	path := "/v3/pay/transactions/out-trade-no/" + url.PathEscape(o.TradeNo) + "?mchid=" + url.QueryEscape(w.cfg.MchID)
	err := w.do(ctx, http.MethodGet, path, nil, &t)
	var apiErr *apiError
	if errors.As(err, &apiErr) && apiErr.Code == "ORDER_NOT_EXIST" {
		return &provider.Payment{TradeNo: o.TradeNo}, nil
	}
	if err != nil {
		return nil, err
	}
	return t.payment(), nil
}

func (w *Wechat) Refund(ctx context.Context, req *provider.RefundRequest) error {
	body := map[string]any{
		"out_trade_no":  req.Order.TradeNo,
		"out_refund_no": req.RefundNo,
		"amount": map[string]any{
			"refund":   int64(req.Amount),
			"total":    req.Order.PayAmount,
			"currency": "CNY",
		},
	}
	var resp struct {
		Status string `json:"status"`
	}
	if err := w.do(ctx, http.MethodPost, "/v3/refund/domestic/refunds", body, &resp); err != nil {
		return err
	}
	if resp.Status == "CLOSED" || resp.Status == "ABNORMAL" {
		return fmt.Errorf("微信退款失败，状态 %s", resp.Status)
	}
	return nil
}

// apiError 微信支付接口错误响应。
type apiError struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *apiError) Error() string {
	return fmt.Sprintf("微信支付返回错误: %s (%s, HTTP %d)", e.Message, e.Code, e.Status)
}

// do 调用 APIv3 接口：签名请求 → 校验响应签名 → 解析 JSON。
func (w *Wechat) do(ctx context.Context, method, path string, in, out any) error {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	auth, err := w.authorization(method, path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Wechatpay-Serial", w.cfg.PublicKeyID)

	resp, err := w.http.Do(req)
	if err != nil {
		return fmt.Errorf("请求微信支付失败: %w", err)
	}
	respBody, err := provider.ReadBody(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		e := &apiError{Status: resp.StatusCode}
		json.Unmarshal(respBody, e)
		return e
	}
	if err := w.verify(resp.Header, respBody); err != nil {
		return fmt.Errorf("微信支付响应%w", err)
	}
	if out != nil && len(respBody) > 0 {
		return json.Unmarshal(respBody, out)
	}
	return nil
}

// authorization 生成请求签名头：
// 签名串 = 方法\nURL路径(含查询串)\n时间戳\n随机串\n请求体\n
func (w *Wechat) authorization(method, path string, body []byte) (string, error) {
	nonce := randomHex(16)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	message := method + "\n" + path + "\n" + ts + "\n" + nonce + "\n" + string(body) + "\n"
	sig, err := keyutil.Sign(w.priv, []byte(message))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",signature="%s",timestamp="%s",serial_no="%s"`,
		w.cfg.MchID, nonce, sig, ts, w.cfg.SerialNo), nil
}

// verify 校验微信支付的应答/回调签名：签名串 = 时间戳\n随机串\n报文主体\n
func (w *Wechat) verify(h http.Header, body []byte) error {
	serial := h.Get("Wechatpay-Serial")
	if serial != w.cfg.PublicKeyID {
		return fmt.Errorf("公钥 ID 不匹配（收到 %q）", serial)
	}
	ts := h.Get("Wechatpay-Timestamp")
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return errors.New("缺少签名时间戳")
	}
	if d := time.Since(time.Unix(sec, 0)); d > maxClockSkew || d < -maxClockSkew {
		return errors.New("签名时间戳已过期")
	}
	message := ts + "\n" + h.Get("Wechatpay-Nonce") + "\n" + string(body) + "\n"
	return keyutil.Verify(w.pub, []byte(message), h.Get("Wechatpay-Signature"))
}

// decryptAESGCM 使用 APIv3 密钥解密回调资源（AEAD_AES_256_GCM）。
func decryptAESGCM(key, nonce, associatedData, ciphertext string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, len(nonce))
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, []byte(nonce), data, []byte(associatedData))
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
