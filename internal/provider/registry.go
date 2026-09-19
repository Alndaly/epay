package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Options 渠道配置的解码器。
type Options interface {
	Decode(v any) error
}

// Factory 根据配置构造渠道实例。
type Factory func(opts Options) (Provider, error)

// FieldType 配置项在管理后台中的输入控件类型。
type FieldType string

const (
	FieldText     FieldType = "text"     // 单行文本
	FieldTextarea FieldType = "textarea" // 多行文本（密钥、证书）
	FieldSelect   FieldType = "select"   // 下拉选择
	FieldSwitch   FieldType = "switch"   // 开关（bool）
	FieldNumber   FieldType = "number"   // 数字（float）
	FieldTags     FieldType = "tags"     // 字符串数组，逗号分隔输入
)

// FieldOption 下拉选项。
type FieldOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Field 描述驱动的一个配置项，管理后台据此自动渲染配置表单。
// Key 必须与驱动 Config 结构体的 yaml 标签一致。
type Field struct {
	Key         string        `json:"key"`
	Label       string        `json:"label"`
	Type        FieldType     `json:"type"`
	Required    bool          `json:"required,omitempty"`
	Secret      bool          `json:"secret,omitempty"` // 敏感字段：读取时脱敏，未修改时保留原值
	Placeholder string        `json:"placeholder,omitempty"`
	Help        string        `json:"help,omitempty"`
	Options     []FieldOption `json:"options,omitempty"`
	Default     any           `json:"default,omitempty"`
}

// Driver 一个上游渠道驱动：元数据 + 构造函数。
type Driver struct {
	Name        string  `json:"name"`        // 驱动名，配置中 driver 字段的取值
	Title       string  `json:"title"`       // 展示名称
	Description string  `json:"description"` // 简介
	DefaultType string  `json:"defaultType"` // 推荐的易支付 type（如 alipay / wxpay）
	Webhook     string  `json:"webhook"`     // 需要在渠道后台配置回调时的说明，为空表示回调地址随下单自动传递
	Fields      []Field `json:"fields"`
	New         Factory `json:"-"`
}

var (
	mu      sync.RWMutex
	drivers = map[string]Driver{}
)

// Register 注册渠道驱动，通常在驱动包的 init() 中调用。
func Register(d Driver) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := drivers[d.Name]; dup {
		panic("provider: 重复注册驱动 " + d.Name)
	}
	drivers[d.Name] = d
}

// Lookup 按名称查找驱动。
func Lookup(name string) (Driver, bool) {
	mu.RLock()
	defer mu.RUnlock()
	d, ok := drivers[name]
	return d, ok
}

// Drivers 返回全部已注册驱动（按名称排序）。
func Drivers() []Driver {
	mu.RLock()
	defer mu.RUnlock()
	list := make([]Driver, 0, len(drivers))
	for _, d := range drivers {
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// New 按驱动名创建渠道实例。
func New(driver string, opts Options) (Provider, error) {
	d, ok := Lookup(driver)
	if !ok {
		names := make([]string, 0)
		for _, d := range Drivers() {
			names = append(names, d.Name)
		}
		return nil, fmt.Errorf("未知的支付驱动 %q（可用：%s）", driver, strings.Join(names, ", "))
	}
	if opts == nil {
		opts = JSONOptions(nil)
	}
	return d.New(opts)
}

// JSONOptions 把 JSON 格式的渠道配置包装为 Options。
//
// 驱动的 Config 结构体使用 yaml 标签，而 JSON 是 YAML 的子集，
// 因此直接用 YAML 解码器解析即可复用同一套标签；KnownFields 模式下拼错的字段会报错。
func JSONOptions(raw json.RawMessage) Options { return jsonOptions(raw) }

type jsonOptions []byte

func (o jsonOptions) Decode(v any) error {
	if len(bytes.TrimSpace(o)) == 0 || string(o) == "null" {
		return nil
	}
	dec := yaml.NewDecoder(bytes.NewReader(o))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("渠道配置错误: %w", err)
	}
	return nil
}
