package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
)

const cookieName = "infera_session"

// Manager 单用户密码门：密码常量时间校验 + HMAC 签名 cookie。
type Manager struct {
	password string
}

// New 构造 Manager。password 为空时 Verify 永远返回 false（无法登录）。
func New(password string) *Manager { return &Manager{password: password} }

// Verify 常量时间比较密码；空密码配置时拒绝一切登录。
func (m *Manager) Verify(pw string) bool {
	if m.password == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(pw), []byte(m.password)) == 1
}

func (m *Manager) signedToken() string {
	mac := hmac.New(sha256.New, []byte(m.password))
	mac.Write([]byte("infera-session-v1"))
	return hex.EncodeToString(mac.Sum(nil))
}

// SetLogin 种 httpOnly 签名 cookie。
func (m *Manager) SetLogin(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: m.signedToken(), Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

// Clear 清 cookie（登出）。
func (m *Manager) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
}

// IsLoggedIn 校验请求里的签名 cookie。
func (m *Manager) IsLoggedIn(r *http.Request) bool {
	if m.password == "" {
		return false
	}
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(m.signedToken())) == 1
}

// Middleware 挡在需要登录的路由前；未登录返回 401。
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.IsLoggedIn(r) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	})
}
