package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/git"
	"github.com/tokfinity/infera/internal/store"
)

// TestApproveBodyReadErrorRejected：approve 的请求体读失败（客户端断连等）
// 必须按 400 拒绝，不得吞错按空 body 继续把门禁批掉。
// 直接调 handler + 注入 chi 路由参数：让 r.Body 本身读失败。
func TestApproveBodyReadErrorRejected(t *testing.T) {
	st := store.NewMemory()
	ctx := context.Background()
	p := &store.Project{Name: "p", RepoURL: "https://github.com/x/y"}
	require.NoError(t, st.CreateProject(ctx, p))
	d := &store.Delivery{ProjectID: p.ID, Title: "x", Status: "active", PendingGate: "spec_approval", CurrentStage: "spec_approval"}
	require.NoError(t, st.CreateDelivery(ctx, d))

	srv := NewServer(st, "secret-pass", &fakeEngine{})
	req := httptest.NewRequest(http.MethodPost, "/api/deliveries/"+d.ID+"/approve", errReader{})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", d.ID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	srv.handleApprove(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code, "读体失败必须 400")
	// 门禁未被消费。
	got, err := st.GetDelivery(ctx, d.ID)
	require.NoError(t, err)
	require.Equal(t, "spec_approval", got.PendingGate)
}

// errReader 一读就失败（模拟请求体传输中断）。
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("client aborted mid-body") }

// TestGateNoPendingMessageUnified：无门禁时 gate 接口的错误文案统一中文，
// 且错误结构带机器可读 code。
func TestGateNoPendingMessageUnified(t *testing.T) {
	ts, st, _ := newServerWithEngine(t)
	p := &store.Project{Name: "p", RepoURL: "https://github.com/x/y"}
	require.NoError(t, st.CreateProject(context.Background(), p))
	d := &store.Delivery{ProjectID: p.ID, Title: "x", Status: "active", CurrentStage: "spec"}
	require.NoError(t, st.CreateDelivery(context.Background(), d))

	c := login(t, ts.URL)
	resp, err := c.Get(ts.URL + "/api/deliveries/" + d.ID + "/gate")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var out struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.NotRegexp(t, `[a-z]{3}\s[a-z]{3}`, out.Error, "文案不应是英文短语")
	require.Equal(t, "invalid_request", out.Code)
}

// TestCreateEndpointsReturn201：资源创建类端点统一 201 Created。
func TestCreateEndpointsReturn201(t *testing.T) {
	ts, _, _ := newServerWithEngine(t)
	c := login(t, ts.URL)

	resp, err := c.Post(ts.URL+"/api/agents", "application/json",
		bytes.NewBufferString(`{"name":"a1","runner":"local"}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	tmp := t.TempDir()
	resp2, err := c.Post(ts.URL+"/api/projects", "application/json",
		bytes.NewBufferString(`{"name":"p1","repo_url":"`+tmp+`"}`))
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusCreated, resp2.StatusCode)

	var p store.Project
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&p))
	require.NotEmpty(t, p.ID)
}

// TestCreateProjectLsRemoteErrorNotLeaked：仓库校验失败回固定文案，
// 原始 git 错误（可能含服务器本地路径）只进日志、不进响应。
func TestCreateProjectLsRemoteErrorNotLeaked(t *testing.T) {
	st := store.NewMemory()
	srv := NewServer(st, "secret-pass", nil)
	srv.SetGit(git.New()) // 真实 git：指向不存在的本地路径，错误信息含该路径
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	c := login(t, ts.URL)

	missing := "/nonexistent-" + t.Name() + "/repo.git"
	resp, err := c.Post(ts.URL+"/api/projects", "application/json",
		bytes.NewBufferString(`{"name":"p","repo_url":"`+missing+`"}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var out struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.NotContains(t, out.Error, missing, "原始错误（含本地路径）不得回显")
	require.Contains(t, out.Error, "仓库")
}

// TestCreateProjectRepoURLWhitelist：repo_url 只接受 https/ssh/本地绝对路径；
// 其它 scheme（http/file/ftp/相对路径）在可达性校验前直接拒绝（SSRF-adjacent）。
func TestCreateProjectRepoURLWhitelist(t *testing.T) {
	srv := NewServer(store.NewMemory(), "secret-pass", nil)
	srv.SetGit(git.New())
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	c := login(t, ts.URL)

	for _, raw := range []string{
		"http://169.254.169.254/latest/meta-data",
		"file:///etc/passwd",
		"ftp://example.com/x",
		"relative/path",
		"git://example.com/x",
	} {
		resp, err := c.Post(ts.URL+"/api/projects", "application/json",
			bytes.NewBufferString(`{"name":"p","repo_url":"`+raw+`"}`))
		require.NoError(t, err)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, "repo_url %q 应被白名单拒绝", raw)
		require.NotContains(t, string(body), "退出码", "应在 LsRemote 之前拦截（不是 git 报错）")
	}

	// https 形态通过白名单、进入可达性校验（localhost:1 立即连接拒绝，无外网依赖）。
	resp, err := c.Post(ts.URL+"/api/projects", "application/json",
		bytes.NewBufferString(`{"name":"p","repo_url":"https://localhost:1/x.git"}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var out struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Contains(t, out.Error, "仓库", "https 走可达性校验路径（白名单放行）")
	require.NotContains(t, out.Error, "localhost:1", "校验错误也是固定文案")
}

// TestValidRepoURL：白名单函数的表驱动单测。
func TestValidRepoURL(t *testing.T) {
	valid := []string{
		"https://github.com/x/y.git",
		"ssh://git@github.com/x/y.git",
		"git@github.com:x/y.git",
		"/tmp/local/repo.git",
	}
	invalid := []string{
		"http://github.com/x/y.git",
		"file:///etc/passwd",
		"ftp://x/y",
		"git://github.com/x/y",
		"relative/path",
		"",
		"javascript:alert(1)",
	}
	for _, u := range valid {
		require.True(t, validRepoURL(u), "%q 应合法", u)
	}
	for _, u := range invalid {
		require.False(t, validRepoURL(u), "%q 应拒绝", u)
	}
}
