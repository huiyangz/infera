package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVerifyPassword(t *testing.T) {
	m := New("s3cret")
	assert.True(t, m.Verify("s3cret"))
	assert.False(t, m.Verify("wrong"))
	assert.False(t, m.Verify(""))
}

func TestLoginCookieRoundTrip(t *testing.T) {
	m := New("s3cret")
	rec := httptest.NewRecorder()
	m.SetLogin(rec)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(rec.Result().Cookies()[0])
	assert.True(t, m.IsLoggedIn(req))
}

func TestWrongCookieNotLoggedIn(t *testing.T) {
	m := New("s3cret")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "infera_session", Value: "garbage"})
	assert.False(t, m.IsLoggedIn(req))
}

func TestMiddlewareBlocksAndAllows(t *testing.T) {
	m := New("s3cret")
	called := false
	h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	// 未登录：401，不调用下游
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// 登录后：放行
	rec2 := httptest.NewRecorder()
	loginRec := httptest.NewRecorder()
	m.SetLogin(loginRec)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(loginRec.Result().Cookies()[0])
	h.ServeHTTP(rec2, req)
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec2.Code)
}
