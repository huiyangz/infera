package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/orchestration"
	"github.com/tokfinity/infera/internal/store"
)

// postAgent POST /api/agents，返回响应。
func postAgent(t *testing.T, c *http.Client, base, body string) *http.Response {
	t.Helper()
	r, err := c.Post(base+"/api/agents", "application/json", bytes.NewBufferString(body))
	require.NoError(t, err)
	return r
}

// patchAgent PATCH /api/agents/{id}，返回响应。
func patchAgent(t *testing.T, c *http.Client, base, id, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPatch, base+"/api/agents/"+id, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	require.NoError(t, err)
	return resp
}

// requireFieldErr 断言 400 且 error 信息带字段名。
func requireFieldErr(t *testing.T, resp *http.Response, field string) {
	t.Helper()
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Contains(t, body["error"], field)
}

// TestAgentConfigValidation agent 配置保存预校验：
// cli 必有命令、http 必有 URL、docker 必有镜像、local 无额外要求；4xx 带字段名。
func TestAgentConfigValidation(t *testing.T) {
	ts, st := newServer(t)
	c := login(t, ts.URL)

	// cli 缺 command → 400 config.command
	requireFieldErr(t, postAgent(t, c, ts.URL, `{"name":"x1","runner":"cli"}`), "config.command")
	// cli command 空数组 → 400
	requireFieldErr(t, postAgent(t, c, ts.URL, `{"name":"x1","runner":"cli","config":{"command":[]}}`), "config.command")
	// http 缺 url → 400 config.url
	requireFieldErr(t, postAgent(t, c, ts.URL, `{"name":"x2","runner":"http"}`), "config.url")
	// docker 缺 image → 400 config.image
	requireFieldErr(t, postAgent(t, c, ts.URL, `{"name":"x3","runner":"docker","config":{"command":["claude"]}}`), "config.image")

	// local 无额外要求 → 201
	resp := postAgent(t, c, ts.URL, `{"name":"x4","runner":"local"}`)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	_ = resp.Body.Close()
	// 合法 cli → 201
	resp = postAgent(t, c, ts.URL, `{"name":"x5","runner":"cli","config":{"command":["echo","hi"]}}`)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var a store.Agent
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&a))

	// PATCH 换 runner 未补 config → 合并结果校验失败（提前于交付期）
	requireFieldErr(t, patchAgent(t, c, ts.URL, a.ID, `{"runner":"http"}`), "config.url")
	// PATCH 补上 url → 通过
	resp = patchAgent(t, c, ts.URL, a.ID, `{"runner":"http","config":{"url":"http://localhost:9"}}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
	// PATCH 把配置改坏 → 拒绝
	requireFieldErr(t, patchAgent(t, c, ts.URL, a.ID, `{"runner":"docker","config":{"command":["x"]}}`), "config.image")
	// 被拒后存量配置未被污染（仍是 http + url）
	got, err := st.GetAgent(context.Background(), a.ID)
	require.NoError(t, err)
	require.Equal(t, "http", got.Runner)
	require.Equal(t, "http://localhost:9", got.Config["url"])
}

// TestPipelinePutRejectsInvalidAgentConfig 绑定引用配置不合法的 agent → 保存即 400，
// 且原绑定不被半写（默认与项目级）。
func TestPipelinePutRejectsInvalidAgentConfig(t *testing.T) {
	ts, st := newServer(t)
	c := login(t, ts.URL)
	ctx := context.Background()

	// 存量脏数据：绕过 API 预校验直接种一个缺 command 的 cli agent
	bad := &store.Agent{Name: "legacy-bad", Runner: "cli"}
	require.NoError(t, st.CreateAgent(ctx, bad))
	good := createAgentViaAPI(t, c, ts.URL, "good-cli", "cli")

	p := &store.Project{Name: "demo", RepoURL: "", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, p))

	// 先放一份合法默认
	put := func(url, body string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPut, ts.URL+url, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		require.NoError(t, err)
		return resp
	}
	fullGood := fullBindings(good, nil)
	resp := put("/api/pipeline", fullGood)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	// 默认 PUT 引用 bad → 400 写明字段与节点，原绑定保持全节点 ×good
	resp = put("/api/pipeline", fullBindings(good, map[string]string{"spec": bad.ID}))
	requireFieldErr(t, resp, "config.command")
	defs, err := st.ListBindings(ctx, "")
	require.NoError(t, err)
	require.Len(t, defs, len(orchestration.BindableNodes))
	for _, b := range defs {
		require.Equal(t, good, b.AgentID, "失败的保存不得留下半写")
	}

	// 项目级 PUT 引用 bad → 400，项目无绑定
	resp = put("/api/projects/"+p.ID+"/pipeline", `{"bindings":{"test_gen":"`+bad.ID+`"}}`)
	requireFieldErr(t, resp, "config.command")
	ovs, err := st.ListBindings(ctx, p.ID)
	require.NoError(t, err)
	require.Empty(t, ovs)
}
