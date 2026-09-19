// Package config 加载 YAML 配置文件。
//
// 配置值中可以使用 ${ENV_NAME} 引用环境变量，便于把密钥放在环境变量 / Docker Secret 中。
package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    Server     `yaml:"server"`
	Database  Database   `yaml:"database"`
	Order     Order      `yaml:"order"`
	Log       Log        `yaml:"log"`
	Merchants []Merchant `yaml:"merchants"`
	Channels  []Channel  `yaml:"channels"`
}

type Server struct {
	Listen  string `yaml:"listen"`   // 监听地址，默认 :8080
	BaseURL string `yaml:"base_url"` // 对外访问地址，如 https://pay.example.com
	// TrustProxy 为 true 时从 X-Forwarded-For / X-Real-IP 读取客户端 IP，
	// 仅当网关部署在可信反向代理（Nginx、Caddy、CDN）之后时开启。
	TrustProxy bool `yaml:"trust_proxy"`
}

type Database struct {
	Path string `yaml:"path"` // SQLite 文件路径
}

type Order struct {
	Expire        time.Duration `yaml:"expire"`         // 订单有效期，默认 30m
	NotifyTimeout time.Duration `yaml:"notify_timeout"` // 单次商户通知超时，默认 10s
}

type Log struct {
	Level  string `yaml:"level"`  // debug | info | warn | error
	Format string `yaml:"format"` // text | json
}

type Merchant struct {
	PID  string `yaml:"pid"`
	Key  string `yaml:"key"`
	Name string `yaml:"name"`
}

// Channel 一个支付方式。Type 是商户下单时传的易支付 type，Driver 是上游渠道驱动名。
type Channel struct {
	Type    string    `yaml:"type"`
	Driver  string    `yaml:"driver"`
	Name    string    `yaml:"name"`
	Enabled *bool     `yaml:"enabled"` // 缺省为启用
	Options yaml.Node `yaml:"options"` // 驱动私有配置，由驱动自行解码
}

// IsEnabled 渠道是否启用。
func (c *Channel) IsEnabled() bool { return c.Enabled == nil || *c.Enabled }

// Decode 实现 provider.Options，把 options 节点解码为驱动自己的配置结构。
// 使用 KnownFields 严格模式，拼错的配置项会直接报错而不是被静默忽略。
func (c *Channel) Decode(v any) error {
	if c.Options.Kind == 0 {
		return nil
	}
	data, err := yaml.Marshal(&c.Options)
	if err != nil {
		return err
	}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("渠道 %s 配置错误: %w", c.Type, err)
	}
	return nil
}

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Load 读取并校验配置文件。
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件: %w", err)
	}
	expanded := envRef.ReplaceAllStringFunc(string(raw), func(m string) string {
		return os.Getenv(envRef.FindStringSubmatch(m)[1])
	})

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件: %w", err)
	}
	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) setDefaults() {
	if c.Server.Listen == "" {
		c.Server.Listen = ":8080"
	}
	if c.Database.Path == "" {
		c.Database.Path = "data/epay.db"
	}
	if c.Order.Expire <= 0 {
		c.Order.Expire = 30 * time.Minute
	}
	if c.Order.NotifyTimeout <= 0 {
		c.Order.NotifyTimeout = 10 * time.Second
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	c.Server.BaseURL = strings.TrimRight(c.Server.BaseURL, "/")
}

func (c *Config) validate() error {
	if !strings.HasPrefix(c.Server.BaseURL, "http://") && !strings.HasPrefix(c.Server.BaseURL, "https://") {
		return errors.New("server.base_url 必须配置为网关的公网访问地址（http/https 开头）")
	}
	if len(c.Merchants) == 0 {
		return errors.New("至少需要配置一个商户 merchants")
	}
	if len(c.Channels) == 0 {
		return errors.New("至少需要配置一个支付渠道 channels")
	}
	for _, ch := range c.Channels {
		if ch.Type == "" || ch.Driver == "" {
			return errors.New("channels 中每一项都必须填写 type 与 driver")
		}
	}
	return nil
}
