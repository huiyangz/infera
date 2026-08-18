// MCP 协议层测试：握手 / 工具列举 / 五个工具 round-trip（HTTP 层直打，
// 覆盖鉴权、版本协商、通知、JSON-RPC 错误与 Origin 校验）。
package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/engine"
	"github.com/tokfinity/infera/internal/orchestration"
	"github.com/tokfinity/infera/internal/store"
)

// --- 测试替身 ---

// fakeWS workspace 替身：Path 返回可预测路径，Acquire/Release 空操作。
type fakeWS struct{}

func (fakeWS) Acquire(_ context.Context, _, _, _ string) (string, string, error) { return "", "", nil }
func (fakeWS) Path(id string) string                                             { return "/tmp/infera-workdirs/" + id }
func (fakeWS) Release(string)                                                    {}

type passTR struct{}

func (passTR) RunTests(context.Context, string) (bool, string, error) {
	return true, "PASS", nil
}

// fakeDrive 记录后台推进回调的调用。
type fakeDrive struct {
	mu   sync.Mutex
	ids  []string
	call func(id string) // 可选：同步联动真驱动
}

func (f *fakeDrive) run(id string) {
	f.mu.Lock()
	f.ids = append(f.ids, id)
	f.mu.Unlock()
	if f.call != nil {
		f.call(id)
	}
}

func (f *fakeDrive) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ids...)
}

const testToken = "mcp-test-token"

// newEnv 装配：真 engine（spec/code_review 节点可切 local 绑定）+ mcp server。
func newEnv(t *testing.T, localNodes ...string) (*httptest.Server, *store.Memory, *fakeDrive) {
	t.Helper()
	st := store.NewMemory()
	e := engine.New(st, &echoRunner{}, fakeWS{}, passTR{})
	local := map[string]bool{}
	for _, n := range localNodes {
		local[n] = true
	}
	e.ResolveRunner = func(_ context.Context, _, node string) (agent.Runner, error) {
		if local[node] {
			return nil, orchestration.ErrLocalRunner
		}
		return nil, nil
	}
	drive := &fakeDrive{}
	s := New(st, e, fakeWS{}.Path, testToken)
	s.SetDrive(drive.run)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, st, drive
}

// echoRunner 记录调用并回显角色（非 local 节点的真 runner 替身）。
type echoRunner struct{ calls []string }

func (r *echoRunner) Run(_ context.Context, req agent.Request) (agent.Result, error) {
	r.calls = append(r.calls, req.Role)
	return agent.Result{Output: "echo:" + req.Role}, nil
}

// seed 建项目 + active 交付（默认停在 spec）。
func seed(t *testing.T, st *store.Memory) *store.Delivery {
	t.Helper()
	ctx := context.Background()
	p := &store.Project{Name: "demo", RepoURL: "https://github.com/example/repo.git", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, p))
	d := &store.Delivery{
		ProjectID: p.ID, Title: "加法函数", Description: "实现 add(a,b)",
		Status: "active", CurrentStage: "spec", WorkspaceReady: true,
	}
	require.NoError(t, st.CreateDelivery(ctx, d))
	return d
}

// --- JSON-RPC 便捷封装 ---

type rpcResp struct {
	ID     any             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func post(t *testing.T, ts *httptest.Server, token, origin, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest("POST", ts.URL+"/mcp", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	return r.StatusCode, b
}

func call(t *testing.T, ts *httptest.Server, id int, method string, params any) (*rpcResp, int) {
	t.Helper()
	body := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		body["params"] = params
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	status, b := post(t, ts, testToken, "", string(raw))
	require.Equal(t, http.StatusOK, status, "body: %s", b)
	var resp rpcResp
	require.NoError(t, json.Unmarshal(b, &resp), "body: %s", b)
	require.EqualValues(t, id, resp.ID) // JSON number 解到 any 是 float64
	return &resp, status
}

// callTool 发 tools/call 并解出 (text, isError)；isError=true 时 text 为错误说明。
func callTool(t *testing.T, ts *httptest.Server, name string, args map[string]any) (string, bool) {
	t.Helper()
	resp, _ := call(t, ts, 1, "tools/call", map[string]any{"name": name, "arguments": args})
	if resp.Error != nil {
		return resp.Error.Message, true
	}
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	require.NoError(t, json.Unmarshal(resp.Result, &res), "result: %s", resp.Result)
	require.NotEmpty(t, res.Content)
	require.Equal(t, "text", res.Content[0].Type)
	return res.Content[0].Text, res.IsError
}

// --- 协议层 ---

func TestDisabledWithoutToken(t *testing.T) {
	st := store.NewMemory()
	e := engine.New(st, &echoRunner{}, fakeWS{}, passTR{})
	s := New(st, e, fakeWS{}.Path, "")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	status, b := post(t, ts, "whatever", "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Contains(t, string(b), "INFERA_MCP_TOKEN")
}

func TestAuth(t *testing.T) {
	ts, _, _ := newEnv(t, "spec")
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`

	status, _ := post(t, ts, "", "", init)
	require.Equal(t, http.StatusUnauthorized, status)
	status, _ = post(t, ts, "wrong-token", "", init)
	require.Equal(t, http.StatusUnauthorized, status)
	status, _ = post(t, ts, testToken, "", init)
	require.Equal(t, http.StatusOK, status)
}

func TestOriginCheck(t *testing.T) {
	ts, _, _ := newEnv(t, "spec")
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`

	status, _ := post(t, ts, testToken, "http://evil.example.com", init)
	require.Equal(t, http.StatusForbidden, status)
	// 本机来源放行（前端 vite / 本地页面）
	status, _ = post(t, ts, testToken, "http://localhost:5173", init)
	require.Equal(t, http.StatusOK, status)
}

func TestHandshakeAndVersionNegotiation(t *testing.T) {
	ts, _, _ := newEnv(t, "spec")

	init := func(v string) map[string]any {
		return map[string]any{
			"protocolVersion": v, "capabilities": map[string]any{},
			"clientInfo": map[string]any{"name": "t", "version": "0"},
		}
	}
	for _, v := range []string{"2024-11-05", "2025-03-26", "2025-06-18"} {
		resp, _ := call(t, ts, 1, "initialize", init(v))
		require.Nil(t, resp.Error)
		var result struct {
			ProtocolVersion string `json:"protocolVersion"`
			Capabilities    struct {
				Tools any `json:"tools"`
			} `json:"capabilities"`
			ServerInfo struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
			Instructions string `json:"instructions"`
		}
		require.NoError(t, json.Unmarshal(resp.Result, &result))
		require.Equal(t, v, result.ProtocolVersion) // 支持集内回显
		require.Equal(t, "infera", result.ServerInfo.Name)
		require.NotNil(t, result.Capabilities.Tools)
		require.NotEmpty(t, result.Instructions)
	}
	// 不认识的版本 → 服务端最新支持版本
	resp, _ := call(t, ts, 1, "initialize", init("1999-01-01"))
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	require.NoError(t, json.Unmarshal(resp.Result, &result))
	require.Equal(t, "2025-06-18", result.ProtocolVersion)

	// notifications/initialized → 202 Accepted，空 body
	status, b := post(t, ts, testToken, "", `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	require.Equal(t, http.StatusAccepted, status)
	require.Empty(t, b)

	// ping → 空 result
	resp, _ = call(t, ts, 2, "ping", nil)
	require.Nil(t, resp.Error)
	require.Equal(t, "{}", string(resp.Result))

	// GET（SSE 流）不支持：无状态实现返回 405
	req, _ := http.NewRequest("GET", ts.URL+"/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	r, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer r.Body.Close()
	require.Equal(t, http.StatusMethodNotAllowed, r.StatusCode)
}

func TestToolsList(t *testing.T) {
	ts, _, _ := newEnv(t, "spec")
	resp, _ := call(t, ts, 1, "tools/list", nil)
	require.Nil(t, resp.Error)

	var res struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(resp.Result, &res))
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
		require.NotEmpty(t, tool.Description)
		require.Equal(t, "object", tool.InputSchema["type"])
	}
	for _, want := range []string{"get_context", "submit_stage_output", "get_gate", "approve_gate", "reject_gate"} {
		require.True(t, names[want], "missing tool %s", want)
	}
}

func TestProtocolErrors(t *testing.T) {
	ts, _, _ := newEnv(t, "spec")

	status, b := post(t, ts, testToken, "", `{bad json`)
	require.Equal(t, http.StatusOK, status)
	var resp rpcResp
	require.NoError(t, json.Unmarshal(b, &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, -32700, resp.Error.Code)

	resp2, _ := call(t, ts, 1, "resources/list", nil)
	require.NotNil(t, resp2.Error)
	require.Equal(t, -32601, resp2.Error.Code)

	text, isErr := callTool(t, ts, "no_such_tool", map[string]any{})
	require.True(t, isErr)
	require.Contains(t, text, "no_such_tool")

	// 未知通知：静默接受（202）
	status, _ = post(t, ts, testToken, "", `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{}}`)
	require.Equal(t, http.StatusAccepted, status)
}

// --- 工具 round-trip ---

func TestGetContextRoundTrip(t *testing.T) {
	ts, st, _ := newEnv(t, "spec")
	d := seed(t, st)
	ctx := context.Background()
	require.NoError(t, st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: d.ID, Stage: "spec", Kind: "spec", Content: "# 已有规格",
	}))

	text, isErr := callTool(t, ts, "get_context", map[string]any{"delivery_id": d.ID})
	require.False(t, isErr)

	var c struct {
		Delivery struct {
			ID, Title, Description, Status, CurrentStage string
		} `json:"delivery"`
		Project struct {
			Name          string `json:"name"`
			RepoURL       string `json:"repo_url"`
			DefaultBranch string `json:"default_branch"`
		} `json:"project"`
		Repo struct {
			Workdir string `json:"workdir"`
		} `json:"repo"`
		Artifacts map[string]struct {
			Stage   string `json:"stage"`
			Content string `json:"content"`
		} `json:"artifacts"`
		PendingLocal *struct {
			Node   string `json:"node"`
			Prompt string `json:"prompt"`
		} `json:"pending_local"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &c), "text: %s", text)
	require.Equal(t, d.ID, c.Delivery.ID)
	require.Equal(t, "实现 add(a,b)", c.Delivery.Description)
	require.Equal(t, "demo", c.Project.Name)
	require.Equal(t, "https://github.com/example/repo.git", c.Project.RepoURL)
	require.Equal(t, "main", c.Project.DefaultBranch)
	require.Equal(t, fakeWS{}.Path(d.ID), c.Repo.Workdir)
	require.Equal(t, "# 已有规格", c.Artifacts["spec"].Content)
	require.NotNil(t, c.PendingLocal)
	require.Equal(t, "spec", c.PendingLocal.Node)
	require.Contains(t, c.PendingLocal.Prompt, "实现 add(a,b)")
}

func TestGetContextNotFound(t *testing.T) {
	ts, _, _ := newEnv(t, "spec")
	text, isErr := callTool(t, ts, "get_context", map[string]any{"delivery_id": "00000000-0000-0000-0000-000000000000"})
	require.True(t, isErr)
	require.Contains(t, text, "不存在")
}

func TestSubmitStageOutputRoundTrip(t *testing.T) {
	ts, st, drive := newEnv(t, "spec")
	d := seed(t, st)

	text, isErr := callTool(t, ts, "submit_stage_output", map[string]any{
		"delivery_id": d.ID, "output": "# 规格\n实现 add(a,b)",
	})
	require.False(t, isErr, "text: %s", text)
	require.Contains(t, text, "spec_approval")

	got, err := st.GetDelivery(context.Background(), d.ID)
	require.NoError(t, err)
	require.Equal(t, "spec_approval", got.CurrentStage)
	a, err := st.LatestArtifact(context.Background(), d.ID, "spec")
	require.NoError(t, err)
	require.Equal(t, "# 规格\n实现 add(a,b)", a.Content)
	require.Equal(t, []string{d.ID}, drive.recorded()) // 交回后触发后台推进
}

func TestSubmitStageOutputNotLocal(t *testing.T) {
	ts, st, drive := newEnv(t) // 无 local 绑定
	d := seed(t, st)

	text, isErr := callTool(t, ts, "submit_stage_output", map[string]any{
		"delivery_id": d.ID, "output": "x",
	})
	require.True(t, isErr)
	require.Contains(t, text, "未绑定本机")
	got, err := st.GetDelivery(context.Background(), d.ID)
	require.NoError(t, err)
	require.Equal(t, "spec", got.CurrentStage)
	require.Empty(t, drive.recorded())
}

func TestGetGateRoundTrip(t *testing.T) {
	ts, st, _ := newEnv(t, "spec")
	d := seed(t, st)
	ctx := context.Background()
	require.NoError(t, st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: d.ID, Stage: "spec", Kind: "spec",
		Content: "# 规格\n```infera-complexity\nsmall\n```",
	}))
	d.PendingGate = "spec_approval"
	d.CurrentStage = "spec_approval"
	require.NoError(t, st.UpdateDelivery(ctx, d))

	text, isErr := callTool(t, ts, "get_gate", map[string]any{"delivery_id": d.ID})
	require.False(t, isErr)

	var g struct {
		Gate                 string `json:"gate"`
		ComplexitySuggestion string `json:"complexity_suggestion"`
		AgentOutput          struct {
			Agent  string `json:"agent"`
			Output string `json:"output"`
		} `json:"agent_output"`
	}
	require.NoError(t, json.Unmarshal([]byte(text), &g), "text: %s", text)
	require.Equal(t, "spec_approval", g.Gate)
	require.Equal(t, "small", g.ComplexitySuggestion)
	require.Equal(t, "spec", g.AgentOutput.Agent)
	require.Contains(t, g.AgentOutput.Output, "# 规格")
}

func TestGetGateIncludesDiffForReviewGate(t *testing.T) {
	ts, st, _ := newEnv(t, "code_review")
	d := seed(t, st)
	ctx := context.Background()
	require.NoError(t, st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: d.ID, Stage: "code_review", Kind: "diff", Content: "+ Hello infera",
	}))
	d.PendingGate = "code_review"
	d.CurrentStage = "code_review"
	require.NoError(t, st.UpdateDelivery(ctx, d))

	text, isErr := callTool(t, ts, "get_gate", map[string]any{"delivery_id": d.ID})
	require.False(t, isErr)
	require.Contains(t, text, "+ Hello infera") // 审查门的裁决材料：diff 全文随门禁详情返回
}

func TestGetGateNoPendingGate(t *testing.T) {
	ts, st, _ := newEnv(t, "spec")
	d := seed(t, st)

	text, isErr := callTool(t, ts, "get_gate", map[string]any{"delivery_id": d.ID})
	require.True(t, isErr)
	require.Contains(t, text, "无挂起门禁")
}

func TestApproveGateRoundTrip(t *testing.T) {
	ts, st, drive := newEnv(t, "spec")
	d := seed(t, st)
	ctx := context.Background()
	d.PendingGate = "spec_approval"
	d.CurrentStage = "spec_approval"
	require.NoError(t, st.UpdateDelivery(ctx, d))

	// complexity=large：spec_approval 分岔到 design
	text, isErr := callTool(t, ts, "approve_gate", map[string]any{
		"delivery_id": d.ID, "complexity": "large",
	})
	require.False(t, isErr, "text: %s", text)

	got, err := st.GetDelivery(ctx, d.ID)
	require.NoError(t, err)
	require.Empty(t, got.PendingGate)
	require.Equal(t, "design", got.CurrentStage)
	require.Equal(t, "large", got.Complexity)
	require.Equal(t, []string{d.ID}, drive.recorded()) // 批准后触发后台推进
}

func TestApproveGateWrongOption(t *testing.T) {
	ts, st, _ := newEnv(t, "spec")
	d := seed(t, st)
	ctx := context.Background()
	d.PendingGate = "design_approval"
	d.CurrentStage = "design_approval"
	require.NoError(t, st.UpdateDelivery(ctx, d))

	// complexity 只在 spec_approval 合法 → 单入口校验拒绝且不消费门禁
	_, isErr := callTool(t, ts, "approve_gate", map[string]any{
		"delivery_id": d.ID, "complexity": "small",
	})
	require.True(t, isErr)
	got, err := st.GetDelivery(ctx, d.ID)
	require.NoError(t, err)
	require.Equal(t, "design_approval", got.PendingGate)
}

func TestRejectGateRoundTrip(t *testing.T) {
	ts, st, drive := newEnv(t, "spec")
	d := seed(t, st)
	ctx := context.Background()
	d.PendingGate = "spec_approval"
	d.CurrentStage = "spec_approval"
	require.NoError(t, st.UpdateDelivery(ctx, d))

	text, isErr := callTool(t, ts, "reject_gate", map[string]any{
		"delivery_id": d.ID, "reason": "补充边界情况",
	})
	require.False(t, isErr, "text: %s", text)

	got, err := st.GetDelivery(ctx, d.ID)
	require.NoError(t, err)
	require.Empty(t, got.PendingGate)
	require.Equal(t, "spec", got.CurrentStage) // 打回回退
	require.Equal(t, "补充边界情况", got.RejectReason)
	require.Equal(t, []string{d.ID}, drive.recorded())
}

func TestMissingArgument(t *testing.T) {
	ts, _, _ := newEnv(t, "spec")

	// 缺 delivery_id → 协议层参数错误（JSON-RPC error，非 isError 结果）
	resp, _ := call(t, ts, 1, "tools/call", map[string]any{
		"name": "get_context", "arguments": map[string]any{},
	})
	require.NotNil(t, resp.Error)
	require.Equal(t, -32602, resp.Error.Code)

	// 非法 UUID → 工具执行错误
	_, isErr := callTool(t, ts, "get_context", map[string]any{"delivery_id": "not-a-uuid"})
	require.True(t, isErr)
}
