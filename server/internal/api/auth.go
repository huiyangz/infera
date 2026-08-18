// Package api 的认证部分：单密码登录 + cookie session（内存 map）。
// 单租户内部工具，不引入用户体系；token 用 crypto/rand 生成。
package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
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

	// 登录失败限速（per-IP）：连续失败达 maxFails 锁 lockWindow，
	// 每次失败响应延迟 failDelay（拖慢在线爆破）。测试可调小窗口值。
	fails       map[string]*loginFails
	maxFails    int
	lockWindow  time.Duration
	failDelay   time.Duration
}

// loginFails 某来源 IP 的连续失败计数与锁定截止时间。
type loginFails struct {
	count       int
	lockedUntil time.Time
}

func newSessionManager(password string) *sessionManager {
	return &sessionManager{
		password:   password,
		sessions:   map[string]time.Time{},
		fails:      map[string]*loginFails{},
		maxFails:   5,
		lockWindow: time.Minute,
		failDelay:  750 * time.Millisecond,
	}
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

// setSessionCookie / clearSessionCookie 是 Server 方法：Secure 属性按
// cookieSecure 配置（HTTPS 终端开启，本地 http 开发关闭）。
func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cookieSecure,
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

// clientIP 提取 RemoteAddr 的 host 部分（不含端口）。不信任可伪造的
// X-Forwarded-For——伪造头即可换 per-IP 限速身份。
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// locked 该 IP 是否处于锁定窗口内（窗口过期惰性清零计数）。
func (m *sessionManager) locked(ip string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.fails[ip]
	if f == nil {
		return false
	}
	if time.Now().Before(f.lockedUntil) {
		return true
	}
	if !f.lockedUntil.IsZero() {
		delete(m.fails, ip) // 窗口已过：锁与计数作废
	}
	return false
}

// recordFailure 记一次失败；连续失败达上限进入锁定窗口。
func (m *sessionManager) recordFailure(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.fails[ip]
	if f == nil {
		f = &loginFails{}
		m.fails[ip] = f
	}
	f.count++
	if f.count >= m.maxFails {
		f.lockedUntil = time.Now().Add(m.lockWindow)
	}
}

// clearFailures 成功登录清零该 IP 的失败计数（偶发输错不累积到锁定）。
func (m *sessionManager) clearFailures(ip string) {
	m.mu.Lock()
	delete(m.fails, ip)
	m.mu.Unlock()
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if s.auth.locked(ip) {
		writeError(w, http.StatusTooManyRequests, "尝试次数过多，请稍后再试")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := decode(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	if !s.auth.checkPassword(body.Password) {
		s.auth.recordFailure(ip)
		time.Sleep(s.auth.failDelay) // 失败响应延迟：拖慢在线爆破
		writeError(w, http.StatusUnauthorized, "密码错误")
		return
	}
	s.auth.clearFailures(ip)
	token, err := s.auth.create()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法创建会话")
		return
	}
	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]bool{"logged_in": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if token := s.sessionToken(r); token != "" {
		s.auth.revoke(token)
	}
	s.clearSessionCookie(w)
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
