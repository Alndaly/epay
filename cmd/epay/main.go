// Command epay 启动兼容易支付协议的统一支付网关。
//
//	epay -config config.yaml
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"epay/internal/admin"
	"epay/internal/config"
	"epay/internal/gateway"
	"epay/internal/model"
	_ "epay/internal/provider/all" // 注册全部内置支付驱动
	"epay/internal/server"
	"epay/internal/store"
	"epay/internal/store/sqlite"
	"epay/web"
)

// Version 版本号，构建时通过 -ldflags "-X main.Version=v1.0.0" 注入。
var Version = "dev"

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	if err := run(*configPath); err != nil {
		fmt.Fprintln(os.Stderr, "启动失败:", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	log := newLogger(cfg.Log)

	st, err := sqlite.Open(cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("打开数据库: %w", err)
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := importConfig(ctx, st, cfg, log); err != nil {
		return err
	}
	svc, err := gateway.New(ctx, st, gateway.Options{
		BaseURL:       cfg.Server.BaseURL,
		OrderTTL:      cfg.Order.Expire,
		NotifyTimeout: cfg.Order.NotifyTimeout,
		Logger:        log,
	})
	if err != nil {
		return err
	}

	httpOpts := server.Options{TrustProxy: cfg.Server.TrustProxy}
	if cfg.Admin.Password == "" {
		log.Warn("未设置 admin.password，管理后台未启用")
	} else {
		adm, err := admin.New(ctx, svc, st, admin.Options{
			Username:   cfg.Admin.Username,
			Password:   cfg.Admin.Password,
			TrustProxy: cfg.Server.TrustProxy,
			Version:    Version,
			UI:         web.UI(),
			Logger:     log,
		})
		if err != nil {
			return fmt.Errorf("初始化管理后台: %w", err)
		}
		httpOpts.Admin = adm
		log.Info("管理后台已启用", "url", cfg.Server.BaseURL+"/admin/")
	}

	notifierDone := make(chan struct{})
	go func() {
		defer close(notifierDone)
		svc.RunNotifier(ctx)
	}()

	srv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           server.New(svc, log, httpOpts),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		log.Info("网关已启动", "version", Version, "listen", cfg.Server.Listen, "base_url", cfg.Server.BaseURL)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("正在关闭…")
	}

	// 优雅退出：等待进行中的请求与通知投递完成。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("HTTP 服务关闭异常", "err", err)
	}
	stop()
	<-notifierDone
	return nil
}

// importConfig 首次启动（数据库中没有任何商户与渠道）时，把配置文件中的商户与渠道导入数据库；
// 之后以数据库（管理后台）为准，配置文件中的这两项会被忽略。
func importConfig(ctx context.Context, st store.Store, cfg *config.Config, log *slog.Logger) error {
	if len(cfg.Merchants) == 0 && len(cfg.Channels) == 0 {
		return nil
	}
	merchants, err := st.ListMerchants(ctx)
	if err != nil {
		return err
	}
	channels, err := st.ListChannels(ctx)
	if err != nil {
		return err
	}
	if len(merchants) > 0 || len(channels) > 0 {
		log.Warn("数据库中已有商户或渠道配置，配置文件中的 merchants / channels 已忽略，请在管理后台修改")
		return nil
	}

	for _, m := range cfg.Merchants {
		if len(m.Key) < 16 {
			return fmt.Errorf("商户 %s 的 key 至少 16 位", m.PID)
		}
		err := st.CreateMerchant(ctx, &model.Merchant{PID: m.PID, Key: m.Key, Name: m.Name, Enabled: true})
		if err != nil {
			return fmt.Errorf("导入商户 %s: %w", m.PID, err)
		}
	}
	for i := range cfg.Channels {
		c := &cfg.Channels[i]
		opts, err := c.OptionsJSON()
		if err != nil {
			return err
		}
		err = st.CreateChannel(ctx, &model.ChannelConfig{
			Type: c.Type, Driver: c.Driver, Name: c.Name, Enabled: c.IsEnabled(), Options: opts,
		})
		if err != nil {
			return fmt.Errorf("导入支付渠道 %s: %w", c.Type, err)
		}
	}
	log.Info("已从配置文件导入商户与支付渠道", "merchants", len(cfg.Merchants), "channels", len(cfg.Channels))
	return nil
}

func newLogger(c config.Log) *slog.Logger {
	var level slog.Level
	if err := level.UnmarshalText([]byte(c.Level)); err != nil {
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler = slog.NewTextHandler(os.Stdout, opts)
	if strings.EqualFold(c.Format, "json") {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(h)
}
