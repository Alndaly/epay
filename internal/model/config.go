package model

import (
	"encoding/json"
	"time"
)

// Merchant 接入网关的商户（例如 new-api），在管理后台维护。
type Merchant struct {
	PID       string    `json:"pid"`
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ChannelConfig 一个支付方式的配置：易支付 type 绑定到某个上游驱动。
type ChannelConfig struct {
	ID        int64           `json:"id"`
	Type      string          `json:"type"`    // 易支付 type，商户下单时传入，唯一
	Driver    string          `json:"driver"`  // 上游驱动名，如 alipay / wechat
	Name      string          `json:"name"`    // 展示名称
	Enabled   bool            `json:"enabled"` // 停用后不再接受新订单，但仍处理已有订单的回调
	Options   json.RawMessage `json:"options"` // 驱动私有配置（JSON）
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}
