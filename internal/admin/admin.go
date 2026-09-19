// Package admin 实现网关的可视化管理后台：/admin/api 下的 JSON 接口，以及嵌入的前端页面。
//
// 接口一览（除登录外均需登录）：
//
//	GET    /admin/api/session                      当前登录状态
//	POST   /admin/api/login | /admin/api/logout
//	GET    /admin/api/overview                     概览统计与系统信息
//	GET    /admin/api/drivers                      可用的渠道驱动及其配置项描述
//	GET    /admin/api/channels                     支付渠道列表（敏感字段已脱敏）
//	POST   /admin/api/channels                     新建渠道
//	PUT    /admin/api/channels/{id}                修改渠道
//	PUT    /admin/api/channels/{id}/enabled        启用 / 停用
//	DELETE /admin/api/channels/{id}
//	GET    /admin/api/merchants                    商户列表（密钥已脱敏）
//	POST   /admin/api/merchants                    新建商户（自动生成密钥）
//	PUT    /admin/api/merchants/{pid}
//	GET    /admin/api/merchants/{pid}/key          查看密钥
//	POST   /admin/api/merchants/{pid}/reset-key    重置密钥
//	DELETE /admin/api/merchants/{pid}
//	GET    /admin/api/orders                       订单列表（筛选 + 分页）
//	GET    /admin/api/orders/{trade_no}
//	POST   /admin/api/orders/{trade_no}/renotify   补发商户通知
//	POST   /admin/api/orders/{trade_no}/sync       主动查询上游
package admin

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"strings"

	"epay/internal/gateway"
	"epay/internal/httputil"
	"epay/internal/store"
)

// Options 管理后台配置。
type Options struct {
	Username   string
	Password   string
	TrustProxy bool
	Version    string
	UI         fs.FS // 前端构建产物（dist 目录），为 nil 或缺少 index.html 时提示未构建
	Logger     *slog.Logger
}

type Admin struct {
	svc      *gateway.Service
	store    store.Store
	sessions *sessions
	opts     Options
	log      *slog.Logger
	api      *http.ServeMux
}

// New 创建管理后台 Handler，挂载在 /admin/ 下。
func New(ctx context.Context, svc *gateway.Service, st store.Store, opts Options) (*Admin, error) {
	if opts.Password == "" {
		return nil, errors.New("未设置管理员密码")
	}
	sess, err := newSessions(ctx, st, opts.Username, opts.Password, strings.HasPrefix(svc.BaseURL(), "https://"))
	if err != nil {
		return nil, err
	}
	a := &Admin{svc: svc, store: st, sessions: sess, opts: opts, log: opts.Logger}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/api/session", a.handleSession)
	mux.HandleFunc("POST /admin/api/login", a.handleLogin)
	mux.HandleFunc("POST /admin/api/logout", a.handleLogout)

	auth := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, a.requireLogin(h)) }
	auth("GET /admin/api/overview", a.handleOverview)
	auth("GET /admin/api/drivers", a.handleDrivers)
	auth("GET /admin/api/channels", a.handleListChannels)
	auth("POST /admin/api/channels", a.handleCreateChannel)
	auth("PUT /admin/api/channels/{id}", a.handleUpdateChannel)
	auth("PUT /admin/api/channels/{id}/enabled", a.handleToggleChannel)
	auth("DELETE /admin/api/channels/{id}", a.handleDeleteChannel)
	auth("GET /admin/api/merchants", a.handleListMerchants)
	auth("POST /admin/api/merchants", a.handleCreateMerchant)
	auth("PUT /admin/api/merchants/{pid}", a.handleUpdateMerchant)
	auth("GET /admin/api/merchants/{pid}/key", a.handleMerchantKey)
	auth("POST /admin/api/merchants/{pid}/reset-key", a.handleResetMerchantKey)
	auth("DELETE /admin/api/merchants/{pid}", a.handleDeleteMerchant)
	auth("GET /admin/api/orders", a.handleListOrders)
	auth("GET /admin/api/orders/{trade_no}", a.handleGetOrder)
	auth("POST /admin/api/orders/{trade_no}/renotify", a.handleRenotify)
	auth("POST /admin/api/orders/{trade_no}/sync", a.handleSyncOrder)
	mux.HandleFunc("/admin/api/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusNotFound, "接口不存在")
	})
	a.api = mux
	return a, nil
}

func (a *Admin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/admin/api/") {
		w.Header().Set("Cache-Control", "no-store")
		a.checkCSRF(a.api).ServeHTTP(w, r)
		return
	}
	a.serveUI(w, r)
}

// checkCSRF 要求写操作使用 JSON 请求体：跨站表单无法在不触发 CORS 预检的情况下发送 JSON，
// 配合 SameSite=Strict 的会话 Cookie 可以有效防御 CSRF。
func (a *Admin) checkCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				fail(w, http.StatusUnsupportedMediaType, "请求必须使用 application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Admin) requireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.sessions.user(r) == "" {
			fail(w, http.StatusUnauthorized, "请先登录")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Admin) handleSession(w http.ResponseWriter, r *http.Request) {
	user := a.sessions.user(r)
	if user == "" {
		fail(w, http.StatusUnauthorized, "请先登录")
		return
	}
	ok(w, map[string]string{"username": user})
}

func (a *Admin) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	ip := httputil.ClientIP(r, a.opts.TrustProxy)
	if err := a.sessions.login(w, ip, req.Username, req.Password); err != nil {
		a.log.Warn("管理后台登录失败", "ip", ip, "username", req.Username)
		fail(w, http.StatusUnauthorized, err.Error())
		return
	}
	a.log.Info("管理后台登录", "ip", ip, "username", req.Username)
	ok(w, map[string]string{"username": req.Username})
}

func (a *Admin) handleLogout(w http.ResponseWriter, _ *http.Request) {
	a.sessions.logout(w)
	ok(w, nil)
}

// serveUI 提供前端单页应用：静态资源直接返回，其余路径回退到 index.html 交给前端路由。
func (a *Admin) serveUI(w http.ResponseWriter, r *http.Request) {
	if a.opts.UI == nil {
		uiMissing(w)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/admin")
	name = strings.TrimPrefix(name, "/")
	if name != "" {
		if data, err := fs.ReadFile(a.opts.UI, name); err == nil {
			if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
				w.Header().Set("Content-Type", ct)
			}
			// Vite 构建的 assets/ 文件名带内容哈希，可以长期缓存。
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			w.Write(data)
			return
		}
	}
	index, err := fs.ReadFile(a.opts.UI, "index.html")
	if err != nil {
		uiMissing(w)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(index)
}

func uiMissing(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	w.Write([]byte("管理后台前端尚未构建：请执行 cd web && pnpm install && pnpm build 后重新编译网关。"))
}
