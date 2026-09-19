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

	"epay/internal/config"
	"epay/internal/gateway"
	"epay/internal/provider"
	_ "epay/internal/provider/all" // 注册全部内置支付驱动
	"epay/internal/server"
	"epay/internal/store/sqlite"
)

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

	channels, err := buildChannels(cfg.Channels, log)
	if err != nil {
		return err
	}
	merchants := make([]gateway.Merchant, 0, len(cfg.Merchants))
	for _, m := range cfg.Merchants {
		merchants = append(merchants, gateway.Merchant{PID: m.PID, Key: m.Key, Name: m.Name})
	}

	svc, err := gateway.New(st, gateway.Options{
		BaseURL:       cfg.Server.BaseURL,
		OrderTTL:      cfg.Order.Expire,
		NotifyTimeout: cfg.Order.NotifyTimeout,
		Merchants:     merchants,
		Channels:      channels,
		Logger:        log,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	notifierDone := make(chan struct{})
	go func() {
		defer close(notifierDone)
		svc.RunNotifier(ctx)
	}()

	srv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           server.New(svc, log, cfg.Server.TrustProxy),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		log.Info("网关已启动", "listen", cfg.Server.Listen, "base_url", cfg.Server.BaseURL)
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

// buildChannels 按配置实例化启用的支付渠道。
func buildChannels(list []config.Channel, log *slog.Logger) ([]gateway.Channel, error) {
	var channels []gateway.Channel
	for i := range list {
		c := &list[i]
		if !c.IsEnabled() {
			continue
		}
		p, err := provider.New(c.Driver, c)
		if err != nil {
			return nil, fmt.Errorf("初始化支付方式 %s: %w", c.Type, err)
		}
		if _, ok := p.(provider.Simulator); ok {
			log.Warn("已启用模拟支付渠道，任何人都可以模拟支付成功，切勿用于生产环境", "type", c.Type)
		}
		channels = append(channels, gateway.Channel{Type: c.Type, Name: c.Name, Provider: p})
		log.Info("支付方式已加载", "type", c.Type, "driver", c.Driver)
	}
	if len(channels) == 0 {
		return nil, errors.New("没有启用的支付渠道")
	}
	return channels, nil
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
