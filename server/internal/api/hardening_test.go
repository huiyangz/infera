package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

// loginRaw 直登返回响应（不消费 cookie，供检查 Set-Cookie 头）。
func loginRaw(t *testing.T, ts *httptest.Server) *http.Response {
	t.Helper()
	r, err := http.Post(ts.URL+"/api/login", "application/json",
		bytes.NewBufferString(`{"password":"secret-pass"}`))
	require.NoError(t, err)
	require.Equal(t, 200, r.StatusCode)
	return r
}

// TestSessionCookieSecureToggle：cookie 的 Secure 属性按配置开关——
// 生产（HTTPS 终端）开启防明文泄露；本地 http 开发关闭（否则 cookie 被浏览器丢弃）。
func TestSessionCookieSecureToggle(t *testing.T) {
	st := store.NewMemory()
	srv := NewServer(st, "secret-pass", nil).SetCookieSecure(true)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	r := loginRaw(t, ts)
	require.Contains(t, r.Header.Get("Set-Cookie"), "Secure", "开启时 Set-Cookie 必须带 Secure")

	st2 := store.NewMemory()
	srv2 := NewServer(st2, "secret-pass", nil) // 默认关闭（本地 http 开发）
	ts2 := httptest.NewServer(srv2.Mux())
	t.Cleanup(ts2.Close)
	r2 := loginRaw(t, ts2)
	require.NotContains(t, r2.Header.Get("Set-Cookie"), "Secure", "默认不启用 Secure")
}

// conflictStore 包装 store：UpdateAgent 第一次调用注入 ErrConflict
// （模拟读-改-写窗口内版本被并发推进 → store 乐观锁拒绝）。
type conflictStore struct {
	store.Store
	fired bool
}

func (c *conflictStore) UpdateAgent(ctx context.Context, a *store.Agent) error {
	if !c.fired {
		c.fired = true
		return store.ErrConflict
	}
	return c.Store.UpdateAgent(ctx, a)
}

// TestPatchAgentStaleWriteConflicts：PATCH 撞上乐观锁（版本过期）→
// 409 + 机器可读 code=conflict，文案提示刷新重试。
func TestPatchAgentStaleWriteConflicts(t *testing.T) {
	inner := store.NewMemory()
	ctx := context.Background()
	created := &store.Agent{Name: "orig", Runner: "cli", Config: map[string]any{"command": []any{"echo", "hi"}}}
	require.NoError(t, inner.CreateAgent(ctx, created))

	srv := NewServer(&conflictStore{Store: inner}, "secret-pass", nil)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	c := login(t, ts.URL)

	body, err := json.Marshal(map[string]any{"name": "renamed"})
	require.NoError(t, err)
	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/agents/"+created.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusConflict, resp.StatusCode, "陈旧版本写入必须 409")

	var out struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Equal(t, "conflict", out.Code)
	require.Contains(t, out.Error, "刷新")
}
