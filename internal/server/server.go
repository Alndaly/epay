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
//	/admin/                     管理后台（前端页面 + /admin/api 接口）
package server

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"epay/internal/gateway"
	"epay/internal/httputil"
)

//go:embed templates/*.html
var templateFS embed.FS

type Server struct {
	svc        *gateway.Service
	log        *slog.Logger
	tpl        *template.Template
	trustProxy bool
}

// Options HTTP 层配置。
type Options struct {
	TrustProxy bool         // 是否信任反向代理传递的客户端 IP
	Admin      http.Handler // 管理后台（挂载在 /admin/），为 nil 时不启用
}

// New 创建 HTTP Handler。
func New(svc *gateway.Service, log *slog.Logger, opts Options) http.Handler {
	s := &Server{
		svc:        svc,
		log:        log,
		tpl:        template.Must(template.ParseFS(templateFS, "templates/*.html")),
		trustProxy: opts.TrustProxy,
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
	if opts.Admin != nil {
		mux.Handle("/admin/", opts.Admin)
		mux.Handle("GET /admin", http.RedirectHandler("/admin/", http.StatusMovedPermanently))
	}

	return s.recoverer(s.accessLog(mux))
}

func (s *Server) clientIP(r *http.Request) string { return httputil.ClientIP(r, s.trustProxy) }

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
		if r.URL.Path == "/healthz" || strings.HasSuffix(r.URL.Path, "/status") ||
			(r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/admin/")) {
			return // 健康检查、收银台轮询与后台页面读取过于频繁，不记录
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
