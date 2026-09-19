package admin

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"epay/internal/store"
)

const (
	cookieName     = "epay_admin"
	sessionTTL     = 7 * 24 * time.Hour
	secretSettings = "admin_session_secret"

	// 登录失败限流：同一 IP 在窗口期内最多失败 loginMaxFails 次。
	loginWindow   = 15 * time.Minute
	loginMaxFails = 10
)

// sessions 管理后台登录态：无状态的签名令牌，存放在 HttpOnly Cookie 中。
//
// 令牌 = base64(载荷) + "." + base64(HMAC-SHA256(载荷))。
// HMAC 密钥由持久化的随机密钥与管理员密码的摘要派生，因此修改密码后所有旧会话立即失效。
type sessions struct {
	username string
	password string
	key      []byte
	secure   bool // 是否仅通过 HTTPS 发送 Cookie

	mu    sync.Mutex
	fails map[string]*failRecord // IP -> 登录失败记录
}

type failRecord struct {
	count int
	since time.Time
}

type sessionPayload struct {
	User string `json:"u"`
	Exp  int64  `json:"e"`
}

func newSessions(ctx context.Context, st store.ConfigStore, username, password string, secure bool) (*sessions, error) {
	secret, err := st.GetSetting(ctx, secretSettings)
	if err != nil {
		return nil, err
	}
	if secret == "" {
		b := make([]byte, 32)
		rand.Read(b)
		secret = hex.EncodeToString(b)
		if err := st.SetSetting(ctx, secretSettings, secret); err != nil {
			return nil, err
		}
	}
	pwd := sha256.Sum256([]byte(password))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(pwd[:])
	return &sessions{
		username: username,
		password: password,
		key:      mac.Sum(nil),
		secure:   secure,
		fails:    map[string]*failRecord{},
	}, nil
}

var errTooManyAttempts = errors.New("登录失败次数过多，请 15 分钟后再试")

// login 校验账号密码；成功时写入会话 Cookie。
func (s *sessions) login(w http.ResponseWriter, ip, username, password string) error {
	if s.blocked(ip) {
		return errTooManyAttempts
	}
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(s.username)) == 1
	pwdOK := subtle.ConstantTimeCompare([]byte(password), []byte(s.password)) == 1
	if !userOK || !pwdOK {
		s.recordFail(ip)
		return errors.New("用户名或密码错误")
	}
	s.clearFails(ip)

	exp := time.Now().Add(sessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    s.sign(sessionPayload{User: username, Exp: exp.Unix()}),
		Path:     "/admin",
		Expires:  exp,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteStrictMode,
	})
	return nil
}

func (s *sessions) logout(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/admin", MaxAge: -1,
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode,
	})
}

// user 返回当前请求的登录用户，未登录或会话无效时返回空字符串。
func (s *sessions) user(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	body, sig, ok := strings.Cut(c.Value, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(s.mac(body))) {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return ""
	}
	var p sessionPayload
	if json.Unmarshal(raw, &p) != nil || time.Now().Unix() > p.Exp {
		return ""
	}
	return p.User
}

func (s *sessions) sign(p sessionPayload) string {
	raw, _ := json.Marshal(p)
	body := base64.RawURLEncoding.EncodeToString(raw)
	return body + "." + s.mac(body)
}

func (s *sessions) mac(body string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *sessions) blocked(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.fails[ip]
	if !ok {
		return false
	}
	if time.Since(f.since) > loginWindow {
		delete(s.fails, ip)
		return false
	}
	return f.count >= loginMaxFails
}

func (s *sessions) recordFail(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.fails[ip]
	if !ok || time.Since(f.since) > loginWindow {
		f = &failRecord{since: time.Now()}
		s.fails[ip] = f
	}
	f.count++
}

func (s *sessions) clearFails(ip string) {
	s.mu.Lock()
	delete(s.fails, ip)
	s.mu.Unlock()
}
