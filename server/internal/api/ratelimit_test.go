package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

// postLogin 发一次登录请求，返回 (状态码, 耗时)。
func postLogin(t *testing.T, base, password string) (int, time.Duration) {
	t.Helper()
	start := time.Now()
	r, err := http.Post(base+"/api/login", "application/json",
		bytes.NewBufferString(`{"password":"`+password+`"}`))
	require.NoError(t, err)
	defer r.Body.Close()
	return r.StatusCode, time.Since(start)
}

// newRateLimitedServer 调小限速参数的测试服务（3 次锁 80ms、失败延迟 1ms）。
func newRateLimitedServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := NewServer(store.NewMemory(), "secret-pass", nil)
	srv.auth.maxFails = 3
	srv.auth.lockWindow = 80 * time.Millisecond
	srv.auth.failDelay = time.Millisecond
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	return ts
}

// TestLoginRateLimitLocksAfterFailures：连续失败达上限后锁定——锁定期内
// 即使密码正确也 429；窗口过后自动解锁，正确密码恢复 200。
func TestLoginRateLimitLocksAfterFailures(t *testing.T) {
	ts := newRateLimitedServer(t)

	for i := 0; i < 3; i++ {
		code, _ := postLogin(t, ts.URL, "wrong")
		require.Equal(t, http.StatusUnauthorized, code, "第 %d 次失败应为 401", i+1)
	}

	// 锁定：正确密码也被拒（429），不再消耗密码比较。
	code, _ := postLogin(t, ts.URL, "secret-pass")
	require.Equal(t, http.StatusTooManyRequests, code)

	time.Sleep(100 * time.Millisecond) // 过锁定窗口
	code, _ = postLogin(t, ts.URL, "secret-pass")
	require.Equal(t, http.StatusOK, code, "窗口过后应恢复登录")
}

// TestLoginSuccessResetsFailCount：失败未达上限时成功登录清零计数——
// 偶发输错不该累积到锁定。
func TestLoginSuccessResetsFailCount(t *testing.T) {
	ts := newRateLimitedServer(t)

	code, _ := postLogin(t, ts.URL, "wrong")
	require.Equal(t, http.StatusUnauthorized, code)
	code, _ = postLogin(t, ts.URL, "secret-pass") // 清零
	require.Equal(t, http.StatusOK, code)
	code, _ = postLogin(t, ts.URL, "wrong")
	require.Equal(t, http.StatusUnauthorized, code)
	code, _ = postLogin(t, ts.URL, "secret-pass") // 仍只累计 1 次，未锁
	require.Equal(t, http.StatusOK, code)
}

// TestLoginFailDelaySlowsBruteForce：每次失败响应至少延迟 failDelay
// （拖慢在线爆破；成功路径不受影响）。
func TestLoginFailDelaySlowsBruteForce(t *testing.T) {
	srv := NewServer(store.NewMemory(), "secret-pass", nil)
	srv.auth.failDelay = 40 * time.Millisecond
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)

	code, elapsed := postLogin(t, ts.URL, "wrong")
	require.Equal(t, http.StatusUnauthorized, code)
	require.GreaterOrEqual(t, elapsed, 40*time.Millisecond, "失败响应必须带延迟")

	code, elapsed = postLogin(t, ts.URL, "secret-pass")
	require.Equal(t, http.StatusOK, code)
	require.Less(t, elapsed, 40*time.Millisecond, "成功路径不受失败延迟拖累")
}
