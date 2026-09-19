package admin

import (
	"crypto/rand"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"epay/internal/model"
)

var pidPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

type merchantView struct {
	model.Merchant
	KeyMasked string `json:"keyMasked"`
}

func maskedMerchant(m model.Merchant) merchantView {
	v := merchantView{Merchant: m, KeyMasked: maskKey(m.Key)}
	v.Key = "" // 列表中不返回明文密钥，需通过单独接口查看
	return v
}

func maskKey(k string) string {
	if len(k) <= 8 {
		return "********"
	}
	return k[:4] + strings.Repeat("*", 8) + k[len(k)-4:]
}

func (a *Admin) handleListMerchants(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.ListMerchants(r.Context())
	if err != nil {
		a.failErr(w, err)
		return
	}
	views := make([]merchantView, 0, len(list))
	for _, m := range list {
		views = append(views, maskedMerchant(m))
	}
	ok(w, views)
}

// handleCreateMerchant 新建商户；pid 留空时自动分配（从 1001 起递增），密钥总是自动生成。
// 响应中包含明文密钥，便于立即复制到 new-api。
func (a *Admin) handleCreateMerchant(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PID  string `json:"pid"`
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	list, err := a.store.ListMerchants(r.Context())
	if err != nil {
		a.failErr(w, err)
		return
	}
	pid := strings.TrimSpace(in.PID)
	if pid == "" {
		pid = nextPID(list)
	} else if !pidPattern.MatchString(pid) {
		fail(w, http.StatusBadRequest, "商户 ID 只能包含字母、数字、下划线和短横线（1-32 位）")
		return
	}
	m := &model.Merchant{PID: pid, Key: randomKey(), Name: strings.TrimSpace(in.Name), Enabled: true}
	if err := a.store.CreateMerchant(r.Context(), m); err != nil {
		a.failErr(w, err)
		return
	}
	if !a.reload(w, r) {
		return
	}
	a.log.Info("新建商户", "pid", m.PID)
	ok(w, merchantView{Merchant: *m, KeyMasked: maskKey(m.Key)})
}

func (a *Admin) handleUpdateMerchant(w http.ResponseWriter, r *http.Request) {
	m, found := a.findMerchant(w, r)
	if !found {
		return
	}
	var in struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	m.Name, m.Enabled = strings.TrimSpace(in.Name), in.Enabled
	if err := a.store.UpdateMerchant(r.Context(), m); err != nil {
		a.failErr(w, err)
		return
	}
	if !a.reload(w, r) {
		return
	}
	a.log.Info("修改商户", "pid", m.PID, "enabled", m.Enabled)
	ok(w, maskedMerchant(*m))
}

func (a *Admin) handleMerchantKey(w http.ResponseWriter, r *http.Request) {
	m, found := a.findMerchant(w, r)
	if !found {
		return
	}
	ok(w, map[string]string{"key": m.Key})
}

func (a *Admin) handleResetMerchantKey(w http.ResponseWriter, r *http.Request) {
	m, found := a.findMerchant(w, r)
	if !found {
		return
	}
	m.Key = randomKey()
	if err := a.store.UpdateMerchant(r.Context(), m); err != nil {
		a.failErr(w, err)
		return
	}
	if !a.reload(w, r) {
		return
	}
	a.log.Warn("重置商户密钥", "pid", m.PID)
	ok(w, map[string]string{"key": m.Key})
}

func (a *Admin) handleDeleteMerchant(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeleteMerchant(r.Context(), r.PathValue("pid")); err != nil {
		a.failErr(w, err)
		return
	}
	if !a.reload(w, r) {
		return
	}
	a.log.Warn("删除商户", "pid", r.PathValue("pid"))
	ok(w, nil)
}

func (a *Admin) findMerchant(w http.ResponseWriter, r *http.Request) (*model.Merchant, bool) {
	list, err := a.store.ListMerchants(r.Context())
	if err != nil {
		a.failErr(w, err)
		return nil, false
	}
	for _, m := range list {
		if m.PID == r.PathValue("pid") {
			return &m, true
		}
	}
	fail(w, http.StatusNotFound, "商户不存在")
	return nil, false
}

func (a *Admin) reload(w http.ResponseWriter, r *http.Request) bool {
	if err := a.svc.Reload(r.Context()); err != nil {
		a.failErr(w, err)
		return false
	}
	return true
}

// nextPID 在现有纯数字商户 ID 的基础上递增，起始为 1001（与常见易支付习惯一致）。
func nextPID(list []model.Merchant) string {
	next := int64(1001)
	for _, m := range list {
		if n, err := strconv.ParseInt(m.PID, 10, 64); err == nil && n >= next {
			next = n + 1
		}
	}
	return strconv.FormatInt(next, 10)
}

// randomKey 生成 32 位字母数字密钥。
func randomKey() string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 32)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b[i] = chars[n.Int64()]
	}
	return string(b)
}
