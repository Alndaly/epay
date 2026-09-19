package server

import (
	"encoding/json"
	"net/http"
	"time"

	"epay/internal/epay"
	"epay/internal/gateway"
	"epay/internal/model"
)

// 易支付协议接口。响应格式与"彩虹易支付"保持一致：code=1 表示成功，其余为失败。

// handleSubmit 页面跳转支付：买家浏览器直接提交表单到此处（new-api 使用此方式）。
func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderMessage(w, http.StatusBadRequest, "请求参数错误", err.Error())
		return
	}
	o, err := s.svc.CreateOrder(r.Context(), gateway.CreateRequest{
		Params:    epay.FromValues(r.Form),
		ClientIP:  s.clientIP(r),
		UserAgent: r.UserAgent(),
	})
	if err != nil {
		s.logInternal(err, "submit 下单失败")
		s.renderMessage(w, http.StatusBadRequest, "下单失败", gateway.PublicMessage(err))
		return
	}
	// 跳转类渠道直接去上游收银台，少一次页面跳转；二维码类进入网关收银台。
	if o.PayKind == model.PayKindRedirect {
		http.Redirect(w, r, o.PayContent, http.StatusFound)
		return
	}
	http.Redirect(w, r, "/pay/"+o.TradeNo, http.StatusFound)
}

// handleMAPI API 下单：商户服务端调用，返回 JSON。
func (s *Server) handleMAPI(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, map[string]any{"code": -1, "msg": "请求参数错误"})
		return
	}
	params := epay.FromValues(r.Form)
	o, err := s.svc.CreateOrder(r.Context(), gateway.CreateRequest{
		Params:   params,
		ClientIP: params["clientip"], // 服务端调用，买家 IP 由商户传入
	})
	if err != nil {
		s.logInternal(err, "mapi 下单失败")
		writeJSON(w, map[string]any{"code": -1, "msg": gateway.PublicMessage(err)})
		return
	}
	resp := map[string]any{"code": 1, "msg": "success", "trade_no": o.TradeNo}
	if o.PayKind == model.PayKindQRCode {
		resp["qrcode"] = o.PayContent
		resp["payurl"] = s.svc.BaseURL() + "/pay/" + o.TradeNo // 同时提供网关收银台，方便商户直接跳转
	} else {
		resp["payurl"] = o.PayContent
	}
	writeJSON(w, resp)
}

// handleAPI 查询与退款接口，通过 act 参数区分：
//
//	act=query   查询商户信息
//	act=order   查询单个订单（trade_no 或 out_trade_no）
//	act=refund  订单退款（money 为退款金额）
func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, map[string]any{"code": -1, "msg": "请求参数错误"})
		return
	}
	m, err := s.svc.AuthMerchant(r.FormValue("pid"), r.FormValue("key"))
	if err != nil {
		writeJSON(w, map[string]any{"code": -1, "msg": gateway.PublicMessage(err)})
		return
	}

	switch r.FormValue("act") {
	case "query":
		writeJSON(w, map[string]any{"code": 1, "pid": m.PID, "name": m.Name, "active": 1})

	case "order":
		o, err := s.svc.MerchantOrder(r.Context(), m, r.FormValue("trade_no"), r.FormValue("out_trade_no"))
		if err != nil {
			s.logInternal(err, "查询订单失败")
			writeJSON(w, map[string]any{"code": -1, "msg": gateway.PublicMessage(err)})
			return
		}
		writeJSON(w, orderJSON(o))

	case "refund":
		err := s.svc.Refund(r.Context(), m, r.FormValue("trade_no"), r.FormValue("out_trade_no"), r.FormValue("money"))
		if err != nil {
			s.logInternal(err, "退款失败")
			writeJSON(w, map[string]any{"code": -1, "msg": gateway.PublicMessage(err)})
			return
		}
		writeJSON(w, map[string]any{"code": 1, "msg": "退款成功"})

	default:
		writeJSON(w, map[string]any{"code": -1, "msg": "不支持的 act"})
	}
}

// orderJSON 按易支付 api.php?act=order 的字段格式输出订单。
func orderJSON(o *model.Order) map[string]any {
	endtime := ""
	if o.Paid() {
		endtime = o.PaidAt.Format(time.DateTime)
	}
	return map[string]any{
		"code":         1,
		"msg":          "查询订单成功",
		"trade_no":     o.TradeNo,
		"out_trade_no": o.OutTradeNo,
		"api_trade_no": o.APITradeNo,
		"type":         o.Type,
		"pid":          o.PID,
		"addtime":      o.CreatedAt.Format(time.DateTime),
		"endtime":      endtime,
		"name":         o.Name,
		"money":        o.Money.String(),
		"refundmoney":  o.RefundMoney.String(),
		"status":       int(o.Status),
		"param":        o.Param,
		"buyer":        o.Buyer,
	}
}

// ---- 上游回调 ----

// handleNotify 接收上游异步通知，并按各渠道要求的格式应答。
func (s *Server) handleNotify(w http.ResponseWriter, r *http.Request) {
	ch, ok := s.svc.Channel(r.PathValue("type"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	err := s.svc.HandleNotify(r.Context(), ch, r)
	ch.Provider.AckNotify(w, err)
}

// handleReturn 买家从上游跳回：主动确认一次支付状态，已支付则带签名参数跳回商户。
func (s *Server) handleReturn(w http.ResponseWriter, r *http.Request) {
	o, err := s.svc.Order(r.Context(), r.PathValue("trade_no"))
	if err != nil {
		s.renderMessage(w, http.StatusNotFound, "订单不存在", gateway.PublicMessage(err))
		return
	}
	if o, err = s.svc.Sync(r.Context(), o); err != nil {
		s.logInternal(err, "同步订单状态失败")
	}
	if !o.Paid() {
		// 尚未确认到账（例如上游通知延迟），回到收银台继续轮询。
		http.Redirect(w, r, "/pay/"+o.TradeNo, http.StatusFound)
		return
	}
	s.redirectToMerchant(w, r, o)
}

// redirectToMerchant 跳转到商户 return_url；未配置时展示支付成功页。
func (s *Server) redirectToMerchant(w http.ResponseWriter, r *http.Request, o *model.Order) {
	target, err := s.svc.ReturnURL(o)
	if err != nil {
		s.logInternal(err, "生成跳转地址失败")
	}
	if target == "" {
		s.renderMessage(w, http.StatusOK, "支付成功", "订单 "+o.OutTradeNo+" 已支付成功，您可以关闭此页面。")
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Server) logInternal(err error, msg string) {
	if err != nil && !gateway.IsPublic(err) {
		s.log.Error(msg, "err", err)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
