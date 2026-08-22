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

// createAgentViaAPI POST /api/agents（创建返回 201），返回 agent id。
func createAgentViaAPI(t *testing.T, c *http.Client, base, name, runner string) string {
	t.Helper()
	r, err := c.Post(base+"/api/agents", "application/json",
		bytes.NewBufferString(`{"name":"`+name+`","runner":"`+runner+`","config":{"command":["echo","hi"]}}`))
	require.NoError(t, err)
	defer r.Body.Close()
	require.Equal(t, http.StatusCreated, r.StatusCode)
	var a store.Agent
	require.NoError(t, json.NewDecoder(r.Body).Decode(&a))
	require.NotEmpty(t, a.ID)
	return a.ID
}

func TestAgentsCRUD(t *testing.T) {
	ts, _ := newServer(t)
	c := login(t, ts.URL)

	// 未认证 401
	r, _ := http.Get(ts.URL + "/api/agents")
	require.Equal(t, 401, r.StatusCode)

	id := createAgentViaAPI(t, c, ts.URL, "default-cli", "cli")

	// 重名 → 409（配置合法才会走到名字冲突检查）
	r, _ = c.Post(ts.URL+"/api/agents", "application/json", bytes.NewBufferString(`{"name":"default-cli","runner":"cli","config":{"command":["echo","hi"]}}`))
	require.Equal(t, http.StatusConflict, r.StatusCode)

	// 非法 runner → 400
	r, _ = c.Post(ts.URL+"/api/agents", "application/json", bytes.NewBufferString(`{"name":"x","runner":"weird"}`))
	require.Equal(t, http.StatusBadRequest, r.StatusCode)

	// PATCH 改名/runner
	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/agents/"+id, bytes.NewBufferString(`{"name":"renamed","runner":"http","config":{"url":"http://localhost:9"}}`))
	req.Header.Set("Content-Type", "application/json")
	r, _ = c.Do(req)
	require.Equal(t, 200, r.StatusCode)
	var a store.Agent
	require.NoError(t, json.NewDecoder(r.Body).Decode(&a))
	require.Equal(t, "renamed", a.Name)
	require.Equal(t, "http", a.Runner)

	// list
	r, _ = c.Get(ts.URL + "/api/agents")
	var list []store.Agent
	require.NoError(t, json.NewDecoder(r.Body).Decode(&list))
	require.Len(t, list, 1)
}

// fullBindings 全节点绑定同一 agent 的 PUT 请求体（跟随 BindableNodes 扩展；
// overrides 覆盖指定节点，如引用不存在的 agent 制造校验失败）。
func fullBindings(base string, overrides map[string]string) string {
	b := map[string]string{}
	for _, n := range orchestration.BindableNodes {
		b[n] = base
	}
	for n, id := range overrides {
		b[n] = id
	}
	raw, err := json.Marshal(map[string]any{"bindings": b})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// TestGlobalPipelineEndpointGone（INFERA-180）：全局默认编排端点已删除——
// GET/PUT /api/pipeline 一律 404，项目级 pipeline 是唯一绑定入口。
func TestGlobalPipelineEndpointGone(t *testing.T) {
	ts, _ := newServer(t)
	c := login(t, ts.URL)

	r, err := c.Get(ts.URL + "/api/pipeline")
	require.NoError(t, err)
	_ = r.Body.Close()
	require.Equal(t, http.StatusNotFound, r.StatusCode, "GET /api/pipeline 应已下线")

	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/pipeline", bytes.NewBufferString(fullBindings("x", nil)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode, "PUT /api/pipeline 应已下线")
}

func TestAgentDeleteConflict(t *testing.T) {
	ts, st := newServer(t)
	c := login(t, ts.URL)
	ctx := context.Background()

	p := &store.Project{Name: "demo", RepoURL: "", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, p))
	id := createAgentViaAPI(t, c, ts.URL, "default-cli", "cli")

	// 项目绑定引用 → 409 且写明节点
	preq, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/projects/"+p.ID+"/pipeline",
		bytes.NewBufferString(fullBindings(id, nil)))
	preq.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(preq)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	_ = resp.Body.Close()

	dreq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/agents/"+id, nil)
	dresp, err := c.Do(dreq)
	require.NoError(t, err)
	require.Equal(t, http.StatusConflict, dresp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(dresp.Body).Decode(&body))
	require.Contains(t, body["error"], "spec")

	// 不存在的 agent → 404
	nreq, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/agents/00000000-0000-0000-0000-000000000000", nil)
	nresp, _ := c.Do(nreq)
	require.Equal(t, http.StatusNotFound, nresp.StatusCode)
}

// TestProjectPipelineShape（INFERA-180 冻结契约）：响应只有 nodes + bindings——
// 项目绑定是唯一绑定来源，无 defaults/overrides/effective/from。
func TestProjectPipelineShape(t *testing.T) {
	ts, st := newServer(t)
	c := login(t, ts.URL)
	ctx := context.Background()

	p := &store.Project{Name: "demo", RepoURL: "", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, p))
	a1 := createAgentViaAPI(t, c, ts.URL, "agent-a", "cli")
	a2 := createAgentViaAPI(t, c, ts.URL, "agent-b", "cli")

	put := func(body string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/projects/"+p.ID+"/pipeline", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		require.NoError(t, err)
		return resp
	}

	// 全量 PUT：响应即 GET 形状
	resp := put(fullBindings(a1, map[string]string{"test_gen": a2}))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var pj struct {
		Nodes    []string          `json:"nodes"`
		Bindings map[string]string `json:"bindings"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&pj))
	require.Equal(t, orchestration.BindableNodes, pj.Nodes)
	require.Len(t, pj.Bindings, len(orchestration.BindableNodes))
	require.Equal(t, a2, pj.Bindings["test_gen"])
	require.Equal(t, a1, pj.Bindings["spec"])

	// GET 同形状
	gresp, err := c.Get(ts.URL + "/api/projects/" + p.ID + "/pipeline")
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, json.NewDecoder(gresp.Body).Decode(&raw))
	for _, key := range []string{"nodes", "bindings"} {
		require.Contains(t, raw, key)
	}
	for _, gone := range []string{"defaults", "overrides", "effective", "from"} {
		require.NotContains(t, raw, gone, "全局默认语义字段必须消失: "+gone)
	}

	// 未知节点 → 400（design 现已是可绑定节点，取真不存在的名字）
	resp = put(`{"bindings":{"nonexistent_stage":"` + a2 + `"}}`)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	_ = resp.Body.Close()

	// 不存在的 agent → 400
	resp = put(fullBindings(a2, map[string]string{"code_gen": "00000000-0000-0000-0000-000000000000"}))
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	_ = resp.Body.Close()

	// {} 清空
	resp = put(`{}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var cleared struct {
		Bindings map[string]string `json:"bindings"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cleared))
	require.Empty(t, cleared.Bindings)

	ovs, err := st.ListBindings(ctx, p.ID)
	require.NoError(t, err)
	require.Empty(t, ovs)

	// 项目不存在 → 404
	greq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/projects/00000000-0000-0000-0000-000000000000/pipeline", nil)
	gresp2, _ := c.Do(greq)
	require.Equal(t, http.StatusNotFound, gresp2.StatusCode)
}
