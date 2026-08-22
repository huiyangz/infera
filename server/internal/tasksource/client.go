// Package tasksource 是对上游任务平台 REST 面的 Go 薄 client（依据接入 spike 实证）。
//
// 只覆盖编排最小闭环：创建 issue → 指派 agent → 轮询 task-runs 终态 → 拉取评论。
// 通用约定：Bearer token 认证；X-Workspace-Id 头隐式必需（每个请求统一注入）；
// 路径无版本前缀 /api/...；本地实例为纯 HTTP。
package tasksource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client 是线程安全的：一次构造，多处复用。
type Client struct {
	http        *http.Client
	baseURL     string // 形如 http://localhost:8088（结尾斜杠已归一）
	token       string // Bearer token（mul_* 用户 token；mat_* agent token 同样可用）
	workspaceID string // 每个请求注入 X-Workspace-Id
}

// New 构造 client 并在构造期挡掉三类误配（坑1/坑4 的入口防线）：
// BaseURL 必须显式给出；token 与 workspace id 缺失直接报错——
// 这些漏到运行期只会变成难排查的 400/401。
func New(baseURL, token, workspaceID string) (*Client, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("tasksource: ServerURL 必须显式配置（如 http://localhost:8088），不内置默认值")
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("tasksource: ServerURL %q 不是合法的 http(s) 地址", baseURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("tasksource: ServerURL %q 必须是 http(s) 地址，got scheme %q", baseURL, u.Scheme)
	}
	if token == "" {
		return nil, fmt.Errorf("tasksource: Token 缺失（Bearer 认证必需）")
	}
	if workspaceID == "" {
		return nil, fmt.Errorf("tasksource: WorkspaceID 缺失——X-Workspace-Id 头隐式必需，漏掉时写接口一律 400")
	}
	return &Client{
		http:        &http.Client{Timeout: 30 * time.Second},
		baseURL:     strings.TrimSuffix(baseURL, "/"),
		token:       token,
		workspaceID: workspaceID,
	}, nil
}

// do 发一个 JSON 请求并把 2xx 响应解码进 out（可为 nil）。
// 认证与 X-Workspace-Id 在这里统一注入——这是全包唯一出口。
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("tasksource: 序列化请求体: %w", err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rd)
	if err != nil {
		return fmt.Errorf("tasksource: 构造请求 %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Workspace-Id", c.workspaceID)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("tasksource: 请求 %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512)) // 回显截断，沿用 agent.HTTPRunner 风格
		return fmt.Errorf("tasksource: %s %s → HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("tasksource: 解码 %s %s 响应: %w", method, path, err)
	}
	return nil
}
