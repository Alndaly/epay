// Package server 是网关的 HTTP 层，只负责协议适配（参数解析、响应格式、页面渲染），
// 业务逻辑全部委托给 gateway.Service。
//
// 路由一览：
//
//	/submit.php                 易支付：页面跳转下单（GET / POST）
//	POST /mapi.php              易支付：API 下单，返回支付链接或二维码
//	/api.php                    易支付：查询商户 / 查询订单 / 退款
//	/notify/{type}              上游异步通知（支付宝 / 微信 / PayPal / Stripe Webhook）
//	GET  /return/{trade_no}     上游同步跳转：确认支付后跳回商户 return_url
//	GET  /pay/{trade_no}        网关收银台（二维码展示 / 继续支付 / 状态轮询）
//	GET  /pay/{trade_no}/qrcode.png
//	GET  /pay/{trade_no}/status
//	GET  /healthz
package server

import (
	"embed"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"epay/internal/gateway"
)

//go:embed templates/*.html
var templateFS embed.FS

type Server struct {
	svc        *gateway.Service
	log        *slog.Logger
	tpl        *template.Template
	trustProxy bool
}

// New 创建 HTTP Handler。
func New(svc *gateway.Service, log *slog.Logger, trustProxy bool) http.Handler {
	s := &Server{
		svc:        svc,
		log:        log,
		tpl:        template.Must(template.ParseFS(templateFS, "templates/*.html")),
		trustProxy: trustProxy,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/submit.php", s.handleSubmit)
	mux.HandleFunc("POST /mapi.php", s.handleMAPI)
	mux.HandleFunc("/api.php", s.handleAPI)
	mux.HandleFunc("POST /notify/{type}", s.handleNotify)
	mux.HandleFunc("GET /return/{trade_no}", s.handleReturn)
	mux.HandleFunc("GET /pay/{trade_no}", s.handleCashier)
	mux.HandleFunc("GET /pay/{trade_no}/qrcode.png", s.handleQRCode)
	mux.HandleFunc("GET /pay/{trade_no}/status", s.handleStatus)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })

	return s.recoverer(s.accessLog(mux))
}

// clientIP 获取买家真实 IP；仅在信任反向代理时读取代理头，防止伪造。
func (s *Server) clientIP(r *http.Request) string {
	if s.trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			ip, _, _ := strings.Cut(xff, ",")
			return strings.TrimSpace(ip)
		}
		if ip := r.Header.Get("X-Real-IP"); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---- 中间件 ----

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if r.URL.Path == "/healthz" || strings.HasSuffix(r.URL.Path, "/status") {
			return // 健康检查与收银台轮询过于频繁，不记录
		}
		s.log.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"ip", s.clientIP(r), "cost", time.Since(start).Round(time.Millisecond))
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("panic", "err", v, "path", r.URL.Path, "stack", string(debug.Stack()))
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
