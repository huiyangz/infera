package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeMCP 模拟 infera MCP 服务（server/internal/mcp 的无状态 JSON-RPC 形态）：
// 校验 Bearer 头，initialize 回协议版本，tools/call 回预置的 get_context 结果。
type fakeMCP struct {
	token    string
	status   int            // 非 0 时直接回该 HTTP 状态（模拟 401/503）
	toolText string         // tools/call 的 text content
	toolErr  string         // 非 0 时以 isError 结果回错误文本
	gotAuth  string         // 记录收到的 Authorization 头
	methods  map[string]int // method → 调用次数
}

func (f *fakeMCP) handler(t *testing.T) http.HandlerFunc {
	if f.methods == nil {
		f.methods = map[string]int{}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		f.gotAuth = r.Header.Get("Authorization")
		if f.status != 0 {
			w.WriteHeader(f.status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "boom"})
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("请求不是合法 JSON-RPC: %v", err)
		}
		f.methods[req.Method]++
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-06-18"}
		case "tools/call":
			if f.toolErr != "" {
				result = map[string]any{
					"content": []any{map[string]string{"type": "text", "text": f.toolErr}},
					"isError": true,
				}
				break
			}
			result = map[string]any{
				"content": []any{map[string]string{"type": "text", "text": f.toolText}},
			}
		default:
			t.Errorf("意外 method: %s", req.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": req.ID, "result": result,
		})
	}
}

const sampleContext = `{
  "delivery": {"id": "d-1", "title": "标题", "description": "描述", "status": "active",
               "current_stage": "spec", "pending_gate": "", "complexity": ""},
  "project": {"name": "p", "repo_url": "http://g/r", "default_branch": "main"},
  "repo": {"workdir": "/tmp/wd", "base_commit": "abc",
           "convention": "workdir 为该交付独占的仓库检出"},
  "artifacts": {"spec": {"stage": "spec", "content": "旧规格"}},
  "pending_gate": {"gate": "", "hint": ""},
  "pending_local": {"node": "spec", "prompt": "你是资深工程师…"}
}`

func TestGetContextRoundTrip(t *testing.T) {
	f := &fakeMCP{token: "sekret", toolText: sampleContext}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()

	c := &Client{Endpoint: srv.URL, Token: "sekret"}
	ctx, err := c.GetContext(context.Background(), "d-1")
	if err != nil {
		t.Fatalf("GetContext 不应报错: %v", err)
	}
	if f.gotAuth != "Bearer sekret" {
		t.Errorf("应带 Bearer 头，得到 %q", f.gotAuth)
	}
	if f.methods["initialize"] != 1 || f.methods["tools/call"] != 1 {
		t.Errorf("应先 initialize 再 tools/call: %+v", f.methods)
	}
	if ctx.Delivery.Title != "标题" || ctx.Delivery.CurrentStage != "spec" {
		t.Errorf("delivery 解析错误: %+v", ctx.Delivery)
	}
	if ctx.Repo.Workdir != "/tmp/wd" {
		t.Errorf("workdir 解析错误: %q", ctx.Repo.Workdir)
	}
	if ctx.PendingLocal == nil || ctx.PendingLocal.Node != "spec" {
		t.Errorf("pending_local 解析错误: %+v", ctx.PendingLocal)
	}
	if ctx.PendingLocal.Prompt != "你是资深工程师…" {
		t.Errorf("pending_local.prompt 解析错误: %q", ctx.PendingLocal.Prompt)
	}
}

func TestGetContextNoPendingLocal(t *testing.T) {
	noLocal := strings.Replace(sampleContext, `"pending_local": {"node": "spec", "prompt": "你是资深工程师…"}`, `"pending_local": null`, 1)
	f := &fakeMCP{token: "sekret", toolText: noLocal}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()

	c := &Client{Endpoint: srv.URL, Token: "sekret"}
	ctx, err := c.GetContext(context.Background(), "d-1")
	if err != nil {
		t.Fatalf("GetContext 不应报错: %v", err)
	}
	if ctx.PendingLocal != nil {
		t.Errorf("pending_local 应为 nil: %+v", ctx.PendingLocal)
	}
}

func TestGetContextToolError(t *testing.T) {
	f := &fakeMCP{token: "sekret", toolErr: "交付不存在"}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()

	c := &Client{Endpoint: srv.URL, Token: "sekret"}
	_, err := c.GetContext(context.Background(), "d-1")
	if err == nil || !strings.Contains(err.Error(), "交付不存在") {
		t.Errorf("工具执行错误应带服务端文本，得到 %v", err)
	}
}

func TestGetContextHTTPStatus(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusServiceUnavailable} {
		f := &fakeMCP{token: "sekret", status: status}
		srv := httptest.NewServer(f.handler(t))
		c := &Client{Endpoint: srv.URL, Token: "sekret"}
		_, err := c.GetContext(context.Background(), "d-1")
		if err == nil {
			t.Errorf("HTTP %d 应报错", status)
		}
		srv.Close()
	}
}

func TestGetContextEmptyDeliveryID(t *testing.T) {
	f := &fakeMCP{token: "sekret", toolText: sampleContext}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()

	c := &Client{Endpoint: srv.URL, Token: "sekret"}
	if _, err := c.GetContext(context.Background(), "  "); err == nil {
		t.Errorf("空 delivery_id 应在客户端侧报错")
	}
	if len(f.methods) != 0 {
		t.Errorf("空 id 不应发起请求: %+v", f.methods)
	}
}
