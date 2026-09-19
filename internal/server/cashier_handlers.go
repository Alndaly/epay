package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"

	"epay/internal/gateway"
	"epay/internal/model"
	"epay/internal/money"
	"epay/internal/provider"
)

// 网关收银台：二维码类渠道在此展示二维码，跳转类渠道在此提供"继续支付"入口；
// 页面通过轮询 /status 获知支付结果并自动跳回商户。

type cashierView struct {
	Order         *model.Order
	ChannelName   string
	QRCode        bool   // 是否展示二维码
	PayURL        string // 跳转类渠道的支付链接
	AppURL        string // 手机端可直接唤起 App 的二维码链接（如支付宝 https://qr.alipay.com/...）
	ForeignAmount string // 外币通道的实付金额，如 "USD 1.39"
	Mobile        bool
	Mock          bool  // 是否为模拟渠道（展示模拟支付按钮）
	ExpireAt      int64 // 过期时间（Unix 毫秒），用于前端倒计时
}

func (s *Server) handleCashier(w http.ResponseWriter, r *http.Request) {
	o, err := s.svc.Order(r.Context(), r.PathValue("trade_no"))
	if err != nil {
		s.renderMessage(w, http.StatusNotFound, "订单不存在", gateway.PublicMessage(err))
		return
	}
	switch {
	case o.Paid():
		s.redirectToMerchant(w, r, o)
		return
	case o.Expired(time.Now()):
		s.renderMessage(w, http.StatusOK, "订单已过期", "请返回商户重新下单。")
		return
	case !o.HasPayment():
		s.renderMessage(w, http.StatusOK, "订单未就绪", "支付渠道下单失败，请返回商户重新下单。")
		return
	}

	view := cashierView{
		Order:       o,
		ChannelName: o.Type,
		QRCode:      o.PayKind == model.PayKindQRCode,
		Mobile:      gateway.DetectDevice("", r.UserAgent()).IsMobile(),
		ExpireAt:    o.ExpireAt.UnixMilli(),
	}
	if o.PayKind == model.PayKindRedirect {
		view.PayURL = o.PayContent
	} else if strings.HasPrefix(o.PayContent, "https://") {
		view.AppURL = o.PayContent
	}
	if o.PayCurrency != "" && o.PayCurrency != "CNY" {
		view.ForeignAmount = o.PayCurrency + " " + money.Cents(o.PayAmount).String()
	}
	if ch, ok := s.svc.Channel(o.Type); ok {
		if ch.Name != "" {
			view.ChannelName = ch.Name
		}
		if sim, ok := ch.Provider.(provider.Simulator); ok && sim.Simulated() {
			view.Mock = true
		}
	}
	s.render(w, http.StatusOK, "cashier.html", view)
}

// handleQRCode 在服务端生成二维码图片，避免依赖第三方前端库或外部服务。
func (s *Server) handleQRCode(w http.ResponseWriter, r *http.Request) {
	o, err := s.svc.Order(r.Context(), r.PathValue("trade_no"))
	if err != nil || o.Paid() || o.PayKind != model.PayKindQRCode {
		http.NotFound(w, r)
		return
	}
	png, err := qrcode.Encode(o.PayContent, qrcode.Medium, 512)
	if err != nil {
		http.Error(w, "生成二维码失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Write(png)
}

// handleStatus 收银台轮询接口；待支付时会（节流地）主动查询上游，作为通知丢失的兜底。
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	o, err := s.svc.Order(r.Context(), r.PathValue("trade_no"))
	if err != nil {
		writeJSON(w, map[string]any{"status": "not_found"})
		return
	}
	if o, err = s.svc.Sync(r.Context(), o); err != nil {
		s.logInternal(err, "同步订单状态失败")
	}
	status := "pending"
	switch {
	case o.Paid():
		status = "paid"
	case o.Expired(time.Now()):
		status = "expired"
	}
	writeJSON(w, map[string]any{"status": status, "return": "/return/" + o.TradeNo})
}

type messageView struct {
	Title   string
	Message string
}

func (s *Server) renderMessage(w http.ResponseWriter, status int, title, msg string) {
	s.render(w, status, "message.html", messageView{Title: title, Message: msg})
}

func (s *Server) render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := s.tpl.ExecuteTemplate(w, name, data); err != nil {
		s.log.Error("渲染页面失败", "template", name, "err", err)
	}
}
