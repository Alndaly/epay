// Package mock 提供一个仅用于开发联调的模拟支付渠道：
// 收银台会展示"模拟支付成功"按钮，点击后走与真实渠道完全相同的回调与通知流程。
//
// 警告：任何人都可以触发模拟支付，切勿在生产环境启用。
package mock

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"epay/internal/model"
	"epay/internal/provider"
)

type Mock struct {
	mu      sync.Mutex
	pending map[string]int64 // trade_no -> 金额，模拟上游保存的订单
}

func init() {
	provider.Register("mock", func(provider.Options) (provider.Provider, error) {
		return &Mock{pending: map[string]int64{}}, nil
	})
}

func (m *Mock) Simulated() bool { return true }

func (m *Mock) Pay(_ context.Context, req *provider.PayRequest) (*provider.PayResult, error) {
	o := req.Order
	m.mu.Lock()
	m.pending[o.TradeNo] = int64(o.Money)
	m.mu.Unlock()
	return &provider.PayResult{
		Kind:     model.PayKindQRCode,
		Content:  "mock://pay/" + o.TradeNo,
		Currency: "CNY",
		Amount:   int64(o.Money),
	}, nil
}

// ParseNotify 收银台的"模拟支付"按钮会以表单 trade_no=xxx 调用通知地址。
func (m *Mock) ParseNotify(_ context.Context, r *http.Request) (*provider.Payment, error) {
	tradeNo := r.FormValue("trade_no")
	m.mu.Lock()
	amount, ok := m.pending[tradeNo]
	m.mu.Unlock()
	if !ok {
		return nil, errors.New("mock: 未知订单（网关重启后需重新下单）")
	}
	return &provider.Payment{
		TradeNo: tradeNo, Paid: true, APITradeNo: "MOCK" + tradeNo,
		Buyer: "mock-buyer", Currency: "CNY", Amount: amount,
	}, nil
}

func (m *Mock) AckNotify(w http.ResponseWriter, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Write([]byte("success"))
}

func (m *Mock) Query(_ context.Context, o *model.Order) (*provider.Payment, error) {
	return &provider.Payment{TradeNo: o.TradeNo}, nil
}

func (m *Mock) Refund(context.Context, *provider.RefundRequest) error { return nil }
