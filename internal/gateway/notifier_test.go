package gateway

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"epay/internal/model"
)

// newTestNotifier 构造一个只用于测试 send 的 Notifier（不依赖存储）。
func newTestNotifier() *Notifier {
	params := func(*model.Order) (map[string]string, error) {
		return map[string]string{"trade_status": "TRADE_SUCCESS", "sign": "0123456789abcdef"}, nil
	}
	return newNotifier(nil, params, 3*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestNotifierSend(t *testing.T) {
	n := newTestNotifier()

	// 商户返回 success（含空白与大小写差异）视为成功
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, " SUCCESS\n")
	}))
	defer ok.Close()
	if err := n.send(t.Context(), &model.Order{NotifyURL: ok.URL}); err != nil {
		t.Fatalf("商户返回 success 应视为成功: %v", err)
	}

	// 返回其他内容视为失败，错误信息带上状态码与响应片段
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer bad.Close()
	err := n.send(t.Context(), &model.Order{NotifyURL: bad.URL})
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("商户返回非 success 应失败: %v", err)
	}
}

// TestNotifierSendErrorOmitsURL 连接失败时的错误信息不应包含完整通知地址
// （里面带着全部签名参数，又长又与失败原因无关，会原样展示在管理后台）。
func TestNotifierSendErrorOmitsURL(t *testing.T) {
	n := newTestNotifier()
	// 关闭的端口：立即连接失败
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := closed.URL
	closed.Close()

	err := n.send(t.Context(), &model.Order{NotifyURL: url})
	if err == nil {
		t.Fatal("连接失败应返回错误")
	}
	if strings.Contains(err.Error(), "sign=") || strings.Contains(err.Error(), "trade_status") {
		t.Fatalf("错误信息不应包含通知参数: %s", err)
	}
	if !strings.Contains(err.Error(), "请求商户失败") {
		t.Fatalf("错误信息应说明是请求商户失败: %s", err)
	}
}
