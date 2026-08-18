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

// fullBindings 全节点绑定同一 agent 的默认编排 PUT 请求体（跟随 BindableNodes 扩展；
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

func TestAgentDeleteConflict(t *testing.T) {
	ts, st := newServer(t)
	c := login(t, ts.URL)
	ctx := context.Background()

	p := &store.Project{Name: "demo", RepoURL: "", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, p))
	id := createAgentViaAPI(t, c, ts.URL, "default-cli", "cli")

	// 默认绑定引用 → 409 且写明节点
	preq, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/pipeline",
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

func TestDefaultPipelinePut(t *testing.T) {
	ts, st := newServer(t)
	c := login(t, ts.URL)
	ctx := context.Background()

	id := createAgentViaAPI(t, c, ts.URL, "default-cli", "cli")

	// 缺节点 → 400 列明
	r, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/pipeline",
		bytes.NewBufferString(`{"bindings":{"spec":"`+id+`"}}`))
	r.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(r)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Contains(t, body["error"], "test_gen")

	// 不存在的 agent → 400
	r, _ = http.NewRequest(http.MethodPut, ts.URL+"/api/pipeline",
		bytes.NewBufferString(fullBindings(id, map[string]string{"code_gen": "00000000-0000-0000-0000-000000000000"})))
	r.Header.Set("Content-Type", "application/json")
	resp, _ = c.Do(r)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// happy：全量替换
	r, _ = http.NewRequest(http.MethodPut, ts.URL+"/api/pipeline",
		bytes.NewBufferString(fullBindings(id, nil)))
	r.Header.Set("Content-Type", "application/json")
	resp, _ = c.Do(r)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	var pipe struct {
		Nodes    []string          `json:"nodes"`
		Agents   []store.Agent     `json:"agents"`
		Bindings map[string]string `json:"bindings"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&pipe))
	require.Len(t, pipe.Nodes, len(orchestration.BindableNodes))
	require.Len(t, pipe.Bindings, len(orchestration.BindableNodes))

	defs, err := st.ListBindings(ctx, "")
	require.NoError(t, err)
	require.Len(t, defs, len(orchestration.BindableNodes))
}

func TestProjectPipelineOverrideAndClear(t *testing.T) {
	ts, st := newServer(t)
	c := login(t, ts.URL)
	ctx := context.Background()

	p := &store.Project{Name: "demo", RepoURL: "", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, p))
	defID := createAgentViaAPI(t, c, ts.URL, "default-cli", "cli")
	ovID := createAgentViaAPI(t, c, ts.URL, "agent-b", "cli")

	// 默认编排
	put := func(body string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/projects/"+p.ID+"/pipeline", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		require.NoError(t, err)
		return resp
	}
	r, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/pipeline",
		bytes.NewBufferString(fullBindings(defID, nil)))
	r.Header.Set("Content-Type", "application/json")
	resp, _ := c.Do(r)
	require.Equal(t, 200, resp.StatusCode)
	_ = resp.Body.Close()

	// 项目只覆盖 test_gen
	resp = put(`{"bindings":{"test_gen":"` + ovID + `"}}`)
	require.Equal(t, 200, resp.StatusCode)
	var pj struct {
		Defaults  map[string]string            `json:"defaults"`
		Overrides map[string]string            `json:"overrides"`
		Effective map[string]map[string]string `json:"effective"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&pj))
	require.Equal(t, ovID, pj.Overrides["test_gen"])
	require.Equal(t, defID, pj.Defaults["test_gen"])
	require.Equal(t, "project", pj.Effective["test_gen"]["from"])
	require.Equal(t, "default", pj.Effective["spec"]["from"])

	// 未知节点 → 400
	resp = put(`{"bindings":{"design":"` + ovID + `"}}`)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	_ = resp.Body.Close()

	// {} 清空覆盖
	resp = put(`{}`)
	require.Equal(t, 200, resp.StatusCode)
	var cleared struct {
		Overrides map[string]string            `json:"overrides"`
		Effective map[string]map[string]string `json:"effective"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&cleared))
	require.Empty(t, cleared.Overrides)
	require.Equal(t, "default", cleared.Effective["test_gen"]["from"])

	ovs, err := st.ListBindings(ctx, p.ID)
	require.NoError(t, err)
	require.Empty(t, ovs)

	// 项目不存在 → 404
	greq, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/projects/00000000-0000-0000-0000-000000000000/pipeline", nil)
	gresp, _ := c.Do(greq)
	require.Equal(t, http.StatusNotFound, gresp.StatusCode)
}
