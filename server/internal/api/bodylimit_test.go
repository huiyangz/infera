package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

// TestDecodeRejectsOversizedBody：请求体超过 1MiB 上限必须 400 拒绝，
// 不得整包读入内存再解析（内存放大防护）。用「合法 JSON 前垫 2MiB 空白」
// 构造——没有上限时会解码成功甚至登录成功（200）。
func TestDecodeRejectsOversizedBody(t *testing.T) {
	ts, _ := newServer(t)

	body := strings.Repeat(" ", 2<<20) + `{"password":"secret-pass"}`
	r, err := http.Post(ts.URL+"/api/login", "application/json", bytes.NewReader([]byte(body)))
	require.NoError(t, err)
	defer r.Body.Close()
	require.Equal(t, http.StatusBadRequest, r.StatusCode, "超限 body 必须在解析前被拒")

	// 对照：1MiB 内的合法请求不受影响（错误密码 → 401，说明已进入正常解码）。
	r2, err := http.Post(ts.URL+"/api/login", "application/json",
		strings.NewReader(`{"password":"wrong"}`))
	require.NoError(t, err)
	defer r2.Body.Close()
	require.Equal(t, http.StatusUnauthorized, r2.StatusCode)

	// 边界：恰好在上限之内（<1MiB）的 body 正常处理。
	bigButOk := strings.Repeat(" ", (1<<20)-64) + `{"password":"wrong"}`
	r3, err := http.Post(ts.URL+"/api/login", "application/json", strings.NewReader(bigButOk))
	require.NoError(t, err)
	defer r3.Body.Close()
	require.Equal(t, http.StatusUnauthorized, r3.StatusCode, "未超限的 body 照常解析")
}

// TestApproveRejectsOversizedBody：handleApprove 手工读 body 也必须受 1MiB
// 上限约束（与 decode 同款 MaxBytesReader）——超限在解析前 400，不得触发
// 引擎 Approve。
func TestApproveRejectsOversizedBody(t *testing.T) {
	st := store.NewMemory()
	eng := &fakeEngine{}
	srv := NewServer(st, "secret-pass", nil)
	srv.SetEngine(eng)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	ctx := context.Background()
	proj := &store.Project{Name: "demo", RepoURL: "https://github.com/x/y", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, proj))
	d := &store.Delivery{
		ProjectID: proj.ID, Title: "x", Status: "active",
		CurrentStage: "spec_approval", PendingGate: "spec_approval",
	}
	require.NoError(t, st.CreateDelivery(ctx, d))
	c := login(t, ts.URL)

	body := strings.Repeat(" ", 2<<20) + `{"complexity":"small"}`
	r, err := c.Post(ts.URL+"/api/deliveries/"+d.ID+"/approve", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer r.Body.Close()
	require.Equal(t, http.StatusBadRequest, r.StatusCode, "超限 body 必须在解析前被拒")
	require.Empty(t, eng.approvedIDs(), "超限 body 不得触发 Approve")

	// 对照：1MiB 内的合法请求照常（复杂度选项被引擎收到）。
	r2, err := c.Post(ts.URL+"/api/deliveries/"+d.ID+"/approve", "application/json",
		strings.NewReader(`{"complexity":"small"}`))
	require.NoError(t, err)
	defer r2.Body.Close()
	require.Equal(t, http.StatusOK, r2.StatusCode)
	require.Equal(t, []string{d.ID}, eng.approvedIDs())
}
