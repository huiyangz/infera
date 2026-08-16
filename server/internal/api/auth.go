// Package api 的认证部分：单密码登录 + cookie session（内存 map）。
// 单租户内部工具，不引入用户体系；token 用 crypto/rand 生成。
package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const (
	sessionCookie = "infera_session"
	sessionTTL    = 7 * 24 * time.Hour
)

// sessionManager 管理内存 session：token -> 过期时间。
type sessionManager struct {
	password string

	mu       sync.Mutex
	sessions map[string]time.Time
}

func newSessionManager(password string) *sessionManager {
	return &sessionManager{password: password, sessions: map[string]time.Time{}}
}

// checkPassword 常数时间比较，避免时序侧信道。
func (m *sessionManager) checkPassword(pw string) bool {
	return subtle.ConstantTimeCompare([]byte(pw), []byte(m.password)) == 1
}

// create 生成随机 token 并登记（顺带清理过期 session，防内存缓慢增长）。
func (m *sessionManager) create() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	now := time.Now()
	m.mu.Lock()
	for t, exp := range m.sessions {
		if now.After(exp) {
			delete(m.sessions, t)
		}
	}
	m.sessions[token] = now.Add(sessionTTL)
	m.mu.Unlock()
	return token, nil
}

func (m *sessionManager) valid(token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(m.sessions, token)
		return false
	}
	return true
}

func (m *sessionManager) revoke(token string) {
	m.mu.Lock()
	delete(m.sessions, token)
	m.mu.Unlock()
}

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// sessionToken 从请求里取合法 token；未登录返回空串。
func (s *Server) sessionToken(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil || !s.auth.valid(c.Value) {
		return ""
	}
	return c.Value
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	if !s.auth.checkPassword(body.Password) {
		writeError(w, http.StatusUnauthorized, "密码错误")
		return
	}
	token, err := s.auth.create()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法创建会话")
		return
	}
	setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]bool{"logged_in": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if token := s.sessionToken(r); token != "" {
		s.auth.revoke(token)
	}
	clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"logged_in": false})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	loggedIn := s.sessionToken(r) != ""
	writeJSON(w, http.StatusOK, map[string]bool{"logged_in": loggedIn})
}

// requireAuth 是认证组中间件：无合法 session 一律 401。
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.sessionToken(r) == "" {
			writeError(w, http.StatusUnauthorized, "未登录")
			return
		}
		next.ServeHTTP(w, r)
	})
}
