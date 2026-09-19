package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"epay/internal/model"
	"epay/internal/store"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOrderLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	o := &model.Order{TradeNo: "T1", OutTradeNo: "O1", PID: "1001", Type: "alipay", Name: "n",
		Money: 1000, NotifyURL: "http://m/n", CreatedAt: time.Now()}

	if err := s.Create(ctx, o); err != nil {
		t.Fatal(err)
	}
	dup := *o
	dup.TradeNo = "T2"
	if err := s.Create(ctx, &dup); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("want ErrDuplicate, got %v", err)
	}

	ok, err := s.MarkPaid(ctx, "T1", "API1", "buyer", time.Now())
	if err != nil || !ok {
		t.Fatalf("MarkPaid = %v, %v", ok, err)
	}
	// 重复回调不应再次变更状态
	if ok, _ := s.MarkPaid(ctx, "T1", "API1", "buyer", time.Now()); ok {
		t.Fatal("second MarkPaid should be a no-op")
	}

	due, err := s.DueNotifications(ctx, time.Now().Add(time.Second), 10)
	if err != nil || len(due) != 1 || due[0].APITradeNo != "API1" {
		t.Fatalf("DueNotifications = %+v, %v", due, err)
	}

	if ok, _ := s.ReserveRefund(ctx, "T1", 600); !ok {
		t.Fatal("first refund should succeed")
	}
	if ok, _ := s.ReserveRefund(ctx, "T1", 500); ok {
		t.Fatal("over-refund should be rejected")
	}
	got, _ := s.GetByOutTradeNo(ctx, "1001", "O1")
	if got.RefundMoney != 600 || !got.Paid() {
		t.Fatalf("unexpected order: %+v", got)
	}
}
