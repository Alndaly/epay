// Package config 加载 YAML 配置文件。
//
// 配置文件只保存"基础设施"级别的配置（监听地址、数据库、管理员账号等）；
// 商户与支付渠道保存在数据库中并通过管理后台维护。配置文件中的 merchants / channels
// 仅在数据库为空时（首次启动）导入一次，便于从纯配置文件部署平滑迁移。
//
// 配置值中可以使用 ${ENV_NAME} 引用环境变量，便于把密钥放在环境变量 / Docker Secret 中。
package config

import (
	"encoding/json"
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
	Admin     Admin      `yaml:"admin"`
	Merchants []Merchant `yaml:"merchants"` // 仅首次启动时导入数据库
	Channels  []Channel  `yaml:"channels"`  // 仅首次启动时导入数据库
}

// Admin 管理后台账号。Password 为空时不启用管理后台。
type Admin struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
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

// OptionsJSON 把 options 节点转换为 JSON，以便存入数据库。
func (c *Channel) OptionsJSON() (json.RawMessage, error) {
	if c.Options.Kind == 0 {
		return json.RawMessage("{}"), nil
	}
	var v map[string]any
	if err := c.Options.Decode(&v); err != nil {
		return nil, fmt.Errorf("渠道 %s 的 options 格式错误: %w", c.Type, err)
	}
	return json.Marshal(v)
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
	if c.Admin.Username == "" {
		c.Admin.Username = "admin"
	}
	c.Server.BaseURL = strings.TrimRight(c.Server.BaseURL, "/")
}

func (c *Config) validate() error {
	if !strings.HasPrefix(c.Server.BaseURL, "http://") && !strings.HasPrefix(c.Server.BaseURL, "https://") {
		return errors.New("server.base_url 必须配置为网关的公网访问地址（http/https 开头）")
	}
	if c.Admin.Password != "" && len(c.Admin.Password) < 8 {
		return errors.New("admin.password 至少 8 位")
	}
	for _, ch := range c.Channels {
		if ch.Type == "" || ch.Driver == "" {
			return errors.New("channels 中每一项都必须填写 type 与 driver")
		}
	}
	return nil
}
