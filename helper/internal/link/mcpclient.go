// MCP 客户端（消费侧）：按 server/internal/mcp 已冻结的协议形态通信——
// 无状态 Streamable HTTP：单 POST /mcp + JSON-RPC 2.0、Bearer 静态 token、
// 不回传会话 ID。GetContext 先 initialize（协议礼节，服务端无状态也不强制）
// 再 tools/call get_context，把 text content 解析为 Context。
package link

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
)

// Client 极简 MCP 客户端。零依赖，仅覆盖 helper 需要的 get_context 一个工具。
type Client struct {
	Endpoint string // MCP 端点（如 http://localhost:8080/mcp）
	Token    string // Bearer token（INFERA_MCP_TOKEN）
	HTTP     *http.Client
}

func (c *Client) httpc() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

var rpcID atomic.Int64

// call 发一个 JSON-RPC 请求并解出 result；HTTP 层与协议层错误都转为 error。
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      rpcID.Add(1),
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.httpc().Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接 MCP 服务失败（%s）: %w", c.Endpoint, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode != http.StatusOK {
		// 服务端错误体是 {"error": "..."}（如未启用/未授权），带出来更可操作。
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return nil, fmt.Errorf("MCP 服务返回 %d: %s", resp.StatusCode, e.Error)
		}
		return nil, fmt.Errorf("MCP 服务返回 %d", resp.StatusCode)
	}
	var rpc struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &rpc); err != nil {
		return nil, fmt.Errorf("MCP 响应不是合法 JSON-RPC: %w", err)
	}
	if rpc.Error != nil {
		return nil, fmt.Errorf("MCP 调用失败: %s", rpc.Error.Message)
	}
	return rpc.Result, nil
}

// toolResult tools/call 的载荷：单 text content，isError=true 表示工具执行错误。
type toolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// callTool initialize 后调一个工具，返回其 text content；工具执行错误转 error。
func (c *Client) callTool(ctx context.Context, name string, args any) (string, error) {
	if _, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "infera-link", "version": "0.1.0"},
	}); err != nil {
		return "", err
	}
	result, err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", err
	}
	var tr toolResult
	if err := json.Unmarshal(result, &tr); err != nil {
		return "", fmt.Errorf("工具结果不合法: %w", err)
	}
	if tr.IsError {
		if len(tr.Content) > 0 {
			return "", fmt.Errorf("%s", tr.Content[0].Text)
		}
		return "", fmt.Errorf("工具 %s 执行失败", name)
	}
	if len(tr.Content) == 0 || tr.Content[0].Text == "" {
		return "", fmt.Errorf("工具 %s 无内容", name)
	}
	return tr.Content[0].Text, nil
}

// Context get_context 结果中 helper 消费的子集（字段名与 server mcp.tools.go 对齐；
// 未知字段忽略，服务端加字段不破坏 helper）。
type Context struct {
	Delivery struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		Description  string `json:"description"`
		Status       string `json:"status"`
		CurrentStage string `json:"current_stage"`
		PendingGate  string `json:"pending_gate"`
		Complexity   string `json:"complexity"`
	} `json:"delivery"`
	Project struct {
		Name          string `json:"name"`
		RepoURL       string `json:"repo_url"`
		DefaultBranch string `json:"default_branch"`
	} `json:"project"`
	Repo struct {
		Workdir    string `json:"workdir"`
		BaseCommit string `json:"base_commit"`
		Convention string `json:"convention"`
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

// GetContext 拉取交付全量驾驶上下文（MCP get_context）。
func (c *Client) GetContext(ctx context.Context, deliveryID string) (*Context, error) {
	if strings.TrimSpace(deliveryID) == "" {
		return nil, fmt.Errorf("delivery_id 不能为空")
	}
	text, err := c.callTool(ctx, "get_context", map[string]string{"delivery_id": deliveryID})
	if err != nil {
		return nil, err
	}
	var out Context
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, fmt.Errorf("get_context 内容不合法: %w", err)
	}
	return &out, nil
}
