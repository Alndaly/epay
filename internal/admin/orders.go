package admin

import (
	"net/http"
	"strconv"
	"time"

	"epay/internal/model"
	"epay/internal/money"
	"epay/internal/provider"
	"epay/internal/store"
)

// orderView 返回给前端的订单：金额统一格式化为两位小数字符串，状态使用可读的枚举值。
type orderView struct {
	TradeNo      string     `json:"tradeNo"`
	OutTradeNo   string     `json:"outTradeNo"`
	PID          string     `json:"pid"`
	Type         string     `json:"type"`
	Name         string     `json:"name"`
	Money        string     `json:"money"`
	Param        string     `json:"param"`
	NotifyURL    string     `json:"notifyUrl"`
	ReturnURL    string     `json:"returnUrl"`
	ClientIP     string     `json:"clientIp"`
	Device       string     `json:"device"`
	Status       string     `json:"status"` // pending | paid | expired
	PayKind      string     `json:"payKind"`
	PayCurrency  string     `json:"payCurrency"`
	PayAmount    string     `json:"payAmount"`
	APITradeNo   string     `json:"apiTradeNo"`
	Buyer        string     `json:"buyer"`
	RefundMoney  string     `json:"refundMoney"`
	NotifyStatus string     `json:"notifyStatus"` // none | pending | success | failed
	NotifyCount  int        `json:"notifyCount"`
	NextNotifyAt *time.Time `json:"nextNotifyAt,omitempty"`
	NotifyError  string     `json:"notifyError"`
	CreatedAt    time.Time  `json:"createdAt"`
	ExpireAt     *time.Time `json:"expireAt,omitempty"`
	PaidAt       *time.Time `json:"paidAt,omitempty"`
}

var (
	notifyNames = map[model.NotifyStatus]string{
		model.NotifyNone: "none", model.NotifyPending: "pending",
		model.NotifySuccess: "success", model.NotifyFailed: "failed",
	}
	notifyValues = map[string]model.NotifyStatus{
		"pending": model.NotifyPending, "success": model.NotifySuccess, "failed": model.NotifyFailed,
	}
)

func toOrderView(o *model.Order, now time.Time) orderView {
	status := "pending"
	switch {
	case o.Paid():
		status = "paid"
	case o.Expired(now):
		status = "expired"
	}
	v := orderView{
		TradeNo: o.TradeNo, OutTradeNo: o.OutTradeNo, PID: o.PID, Type: o.Type, Name: o.Name,
		Money: o.Money.String(), Param: o.Param, NotifyURL: o.NotifyURL, ReturnURL: o.ReturnURL,
		ClientIP: o.ClientIP, Device: o.Device, Status: status, PayKind: string(o.PayKind),
		PayCurrency: o.PayCurrency, PayAmount: money.Cents(o.PayAmount).String(),
		APITradeNo: o.APITradeNo, Buyer: o.Buyer, RefundMoney: o.RefundMoney.String(),
		NotifyStatus: notifyNames[o.NotifyStatus], NotifyCount: o.NotifyCount, NotifyError: o.NotifyError,
		CreatedAt: o.CreatedAt, NextNotifyAt: timePtr(o.NextNotifyAt), ExpireAt: timePtr(o.ExpireAt), PaidAt: timePtr(o.PaidAt),
	}
	if o.NotifyStatus != model.NotifyPending {
		v.NextNotifyAt = nil
	}
	return v
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// handleListOrders 查询参数：keyword、status（pending|paid）、type、pid、notify（pending|success|failed）、page、pageSize。
func (a *Admin) handleListOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("pageSize"))
	page, size = max(page, 1), min(max(size, 1), 100)
	if q.Get("pageSize") == "" {
		size = 20
	}

	f := store.OrderFilter{Keyword: q.Get("keyword"), Type: q.Get("type"), PID: q.Get("pid"),
		Offset: (page - 1) * size, Limit: size}
	switch q.Get("status") {
	case "pending":
		s := model.StatusPending
		f.Status = &s
	case "paid":
		s := model.StatusPaid
		f.Status = &s
	}
	if n, found := notifyValues[q.Get("notify")]; found {
		f.Notify = &n
	}

	list, total, err := a.store.ListOrders(r.Context(), f)
	if err != nil {
		a.failErr(w, err)
		return
	}
	now := time.Now()
	items := make([]orderView, 0, len(list))
	for _, o := range list {
		items = append(items, toOrderView(o, now))
	}
	ok(w, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

func (a *Admin) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	o, err := a.svc.Order(r.Context(), r.PathValue("trade_no"))
	if err != nil {
		a.failErr(w, err)
		return
	}
	ok(w, toOrderView(o, time.Now()))
}

func (a *Admin) handleRenotify(w http.ResponseWriter, r *http.Request) {
	tradeNo := r.PathValue("trade_no")
	if err := a.svc.Renotify(r.Context(), tradeNo); err != nil {
		a.failErr(w, err)
		return
	}
	a.log.Info("手动补发商户通知", "trade_no", tradeNo)
	a.handleGetOrder(w, r)
}

func (a *Admin) handleSyncOrder(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.ForceSync(r.Context(), r.PathValue("trade_no")); err != nil {
		a.failErr(w, err)
		return
	}
	a.handleGetOrder(w, r)
}

// handleOverview 概览：统计数据 + 对接信息。
func (a *Admin) handleOverview(w http.ResponseWriter, r *http.Request) {
	sum, err := a.store.Summary(r.Context(), time.Now(), 30)
	if err != nil {
		a.failErr(w, err)
		return
	}
	merchants, err := a.store.ListMerchants(r.Context())
	if err != nil {
		a.failErr(w, err)
		return
	}
	channels, err := a.store.ListChannels(r.Context())
	if err != nil {
		a.failErr(w, err)
		return
	}
	activeChannels, failedChannels := 0, 0
	for _, c := range channels {
		if a.svc.ChannelError(c.ID) != "" {
			failedChannels++
		} else if c.Enabled {
			activeChannels++
		}
	}
	base := a.svc.BaseURL()
	ok(w, map[string]any{
		"summary": sum,
		"system": map[string]any{
			"version":        a.opts.Version,
			"baseUrl":        base,
			"submitUrl":      base + "/submit.php",
			"mapiUrl":        base + "/mapi.php",
			"apiUrl":         base + "/api.php",
			"merchants":      len(merchants),
			"channels":       len(channels),
			"activeChannels": activeChannels,
			"failedChannels": failedChannels,
			"drivers":        len(provider.Drivers()),
		},
	})
}
