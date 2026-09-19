package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"epay/internal/model"
	"epay/internal/provider"
)

// secretMask 敏感字段在接口中的脱敏占位符；提交时若仍为该值，表示"保持原值不变"。
const secretMask = "******"

// typePattern 易支付 type 会出现在回调路径 /notify/{type} 中，限制为安全字符。
var typePattern = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

type channelView struct {
	model.ChannelConfig
	DriverTitle string `json:"driverTitle"`
	NotifyURL   string `json:"notifyUrl"`       // 上游回调地址（需在渠道后台配置 Webhook 时使用）
	Error       string `json:"error,omitempty"` // 渠道初始化失败的原因
}

type channelInput struct {
	Type    string         `json:"type"`
	Driver  string         `json:"driver"`
	Name    string         `json:"name"`
	Enabled bool           `json:"enabled"`
	Options map[string]any `json:"options"`
}

func (a *Admin) handleDrivers(w http.ResponseWriter, _ *http.Request) {
	ok(w, provider.Drivers())
}

func (a *Admin) handleListChannels(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.ListChannels(r.Context())
	if err != nil {
		a.failErr(w, err)
		return
	}
	views := make([]channelView, 0, len(list))
	for _, c := range list {
		views = append(views, a.channelView(c))
	}
	ok(w, views)
}

func (a *Admin) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	var in channelInput
	if !decode(w, r, &in) {
		return
	}
	cfg, err := a.buildChannel(in, nil)
	if err != nil {
		a.failErr(w, err)
		return
	}
	if err := a.store.CreateChannel(r.Context(), cfg); err != nil {
		a.failErr(w, err)
		return
	}
	a.reloadAndRespond(w, r, cfg.ID, "新建支付渠道")
}

func (a *Admin) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	existing, ok := a.loadChannel(w, r)
	if !ok {
		return
	}
	var in channelInput
	if !decode(w, r, &in) {
		return
	}
	cfg, err := a.buildChannel(in, existing)
	if err != nil {
		a.failErr(w, err)
		return
	}
	if err := a.store.UpdateChannel(r.Context(), cfg); err != nil {
		a.failErr(w, err)
		return
	}
	a.reloadAndRespond(w, r, cfg.ID, "修改支付渠道")
}

func (a *Admin) handleToggleChannel(w http.ResponseWriter, r *http.Request) {
	existing, ok := a.loadChannel(w, r)
	if !ok {
		return
	}
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	existing.Enabled = in.Enabled
	if err := a.store.UpdateChannel(r.Context(), existing); err != nil {
		a.failErr(w, err)
		return
	}
	a.reloadAndRespond(w, r, existing.ID, "切换支付渠道状态")
}

func (a *Admin) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	existing, found := a.loadChannel(w, r)
	if !found {
		return
	}
	if err := a.store.DeleteChannel(r.Context(), existing.ID); err != nil {
		a.failErr(w, err)
		return
	}
	if err := a.svc.Reload(r.Context()); err != nil {
		a.failErr(w, err)
		return
	}
	a.log.Info("删除支付渠道", "type", existing.Type)
	ok(w, nil)
}

func (a *Admin) loadChannel(w http.ResponseWriter, r *http.Request) (*model.ChannelConfig, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		fail(w, http.StatusBadRequest, "渠道 ID 无效")
		return nil, false
	}
	c, err := a.store.GetChannel(r.Context(), id)
	if err != nil {
		a.failErr(w, err)
		return nil, false
	}
	return c, true
}

// reloadAndRespond 让配置立即生效，并返回渠道最新状态（含初始化错误）。
func (a *Admin) reloadAndRespond(w http.ResponseWriter, r *http.Request, id int64, action string) {
	if err := a.svc.Reload(r.Context()); err != nil {
		a.failErr(w, err)
		return
	}
	c, err := a.store.GetChannel(r.Context(), id)
	if err != nil {
		a.failErr(w, err)
		return
	}
	a.log.Info(action, "type", c.Type, "driver", c.Driver, "enabled", c.Enabled)
	ok(w, a.channelView(*c))
}

// buildChannel 校验输入并生成待保存的配置；existing 为 nil 表示新建。
// 保存前会用新配置实际构造一次渠道实例，确保密钥等配置可用。
func (a *Admin) buildChannel(in channelInput, existing *model.ChannelConfig) (*model.ChannelConfig, error) {
	in.Type = strings.TrimSpace(in.Type)
	if !typePattern.MatchString(in.Type) {
		return nil, invalid("支付方式标识只能包含小写字母、数字和下划线（1-32 位）")
	}
	if existing != nil && existing.Type != in.Type {
		// 存量订单与上游回调地址都绑定了 type，修改会导致回调无法匹配。
		return nil, invalid("支付方式标识创建后不可修改")
	}
	driver, found := provider.Lookup(in.Driver)
	if !found {
		return nil, invalid("未知的渠道驱动：" + in.Driver)
	}
	if strings.TrimSpace(in.Name) == "" {
		in.Name = driver.Title
	}

	var previous map[string]any
	if existing != nil && existing.Driver == in.Driver {
		json.Unmarshal(existing.Options, &previous)
	}
	options, err := normalizeOptions(driver, in.Options, previous)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(options)
	if _, err := provider.New(driver.Name, provider.JSONOptions(raw)); err != nil {
		return nil, invalid("配置校验失败：" + err.Error())
	}

	cfg := &model.ChannelConfig{Type: in.Type, Driver: in.Driver, Name: in.Name, Enabled: in.Enabled, Options: raw}
	if existing != nil {
		cfg.ID = existing.ID
	}
	return cfg, nil
}

// normalizeOptions 按驱动的字段描述整理表单提交的配置：
//   - 只保留已声明的字段；
//   - 把字符串形式的数字 / 布尔 / 列表转换为正确类型；
//   - 敏感字段提交脱敏占位符时沿用原值。
func normalizeOptions(d provider.Driver, in, previous map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(d.Fields))
	for _, f := range d.Fields {
		v, present := in[f.Key]
		if f.Secret && v == secretMask {
			v, present = previous[f.Key], previous[f.Key] != nil
		}
		if !present || v == nil {
			if f.Required {
				return nil, invalid(f.Label + "不能为空")
			}
			continue
		}
		cv, err := coerce(f, v)
		if err != nil {
			return nil, err
		}
		if s, isStr := cv.(string); isStr && s == "" {
			if f.Required {
				return nil, invalid(f.Label + "不能为空")
			}
			continue
		}
		out[f.Key] = cv
	}
	return out, nil
}

func coerce(f provider.Field, v any) (any, error) {
	switch f.Type {
	case provider.FieldNumber:
		switch n := v.(type) {
		case float64:
			return n, nil
		case string:
			if strings.TrimSpace(n) == "" {
				return "", nil
			}
			x, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
			if err != nil {
				return nil, invalid(f.Label + "必须是数字")
			}
			return x, nil
		}
	case provider.FieldSwitch:
		if b, isBool := v.(bool); isBool {
			return b, nil
		}
	case provider.FieldTags:
		var tags []string
		switch t := v.(type) {
		case string:
			for _, s := range strings.Split(t, ",") {
				if s = strings.TrimSpace(s); s != "" {
					tags = append(tags, s)
				}
			}
		case []any:
			for _, s := range t {
				if str, isStr := s.(string); isStr && strings.TrimSpace(str) != "" {
					tags = append(tags, strings.TrimSpace(str))
				}
			}
		}
		if len(tags) == 0 {
			return "", nil
		}
		return tags, nil
	default: // text / textarea / select
		if s, isStr := v.(string); isStr {
			return strings.TrimSpace(s), nil
		}
	}
	return nil, invalid(fmt.Sprintf("%s的类型不正确", f.Label))
}

// channelView 生成返回给前端的渠道信息，敏感字段替换为占位符。
func (a *Admin) channelView(c model.ChannelConfig) channelView {
	v := channelView{
		ChannelConfig: c,
		NotifyURL:     a.svc.BaseURL() + "/notify/" + c.Type,
		Error:         a.svc.ChannelError(c.ID),
	}
	d, found := provider.Lookup(c.Driver)
	if !found {
		v.Error = "未知的渠道驱动：" + c.Driver
		return v
	}
	v.DriverTitle = d.Title

	var opts map[string]any
	if json.Unmarshal(c.Options, &opts) == nil {
		for _, f := range d.Fields {
			if s, isStr := opts[f.Key].(string); f.Secret && isStr && s != "" {
				opts[f.Key] = secretMask
			}
		}
		v.Options, _ = json.Marshal(opts)
	}
	return v
}
