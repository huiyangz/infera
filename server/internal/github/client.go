// Package github 是对 GitHub REST API v3 的服务端薄 client（FR-7 GitHub 隐身：
// PR 状态、评审、行级评论、合并全部由 infera 后端代理，用户不访问 GitHub 页面）。
//
// 只覆盖合并闸门最小闭环：PR 元数据 → 行级评审评论 → diff 行数统计 → merge PR。
// 实现为直接 REST 调用（不引 go-github）：无新增依赖，全部可经 httptest 离线单测。
// 通用约定：Bearer token 认证；Accept: application/vnd.github+json；
// X-GitHub-Api-Version: 2022-11-28；User-Agent 必带（GitHub 对无 UA 请求直接 403）。
package github

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

// DefaultBaseURL 是 GitHub REST API 的正式入口；可用 WithBaseURL 覆盖
// （GitHub Enterprise / 测试注入）。
const DefaultBaseURL = "https://api.github.com"

// Client 是线程安全的：一次构造，多处复用。
type Client struct {
	http    *http.Client
	baseURL string // 结尾斜杠已归一
	token   string // Bearer token（PAT / fine-grained 均可）
}

// Option 定制 Client 构造。
type Option func(*Client) error

// WithBaseURL 覆盖 API 入口（须为合法 http(s) 地址；结尾斜杠自动归一）。
func WithBaseURL(u string) Option {
	return func(c *Client) error {
		parsed, err := url.Parse(u)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("github: BaseURL %q 不是合法的 http(s) 地址", u)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("github: BaseURL %q 必须是 http(s) 地址，got scheme %q", u, parsed.Scheme)
		}
		c.baseURL = strings.TrimSuffix(u, "/")
		return nil
	}
}

// WithHTTPClient 注入自定义 *http.Client（超时/Transport 定制）。
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) error {
		if hc == nil {
			return fmt.Errorf("github: WithHTTPClient 不接受 nil")
		}
		c.http = hc
		return nil
	}
}

// New 构造 client 并在构造期挡掉误配：token 缺失直接报错——
// 漏到运行期只会变成难排查的 401。
func New(token string, opts ...Option) (*Client, error) {
	if token == "" {
		return nil, fmt.Errorf("github: Token 缺失（Bearer 认证必需，来自 GITHUB_TOKEN）")
	}
	c := &Client{
		http:    &http.Client{Timeout: 30 * time.Second},
		baseURL: DefaultBaseURL,
		token:   token,
	}
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// APIError 是 GitHub API 返回非 2xx 时的归因类型：调用方 errors.As 取出
// StatusCode / Message，区分"PR 不存在"(404)、"不可合并"(405/409)、
// 鉴权/限流 (401/403/429) 等处置路径。
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Message    string // GitHub 响应体里的 message 字段
}

func (e *APIError) Error() string {
	return fmt.Sprintf("github: %s %s → HTTP %d: %s", e.Method, e.Path, e.StatusCode, e.Message)
}

// do 发一个 JSON 请求并把 2xx 响应解码进 out（可为 nil）。
// 认证与 GitHub 约定头在这里统一注入——这是全包唯一出口。
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("github: 序列化请求体: %w", err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rd)
	if err != nil {
		return fmt.Errorf("github: 构造请求 %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "infera")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("github: 请求 %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512)) // 回显截断，沿用 tasksource client 风格
		apiErr := &APIError{
			Method:     method,
			Path:       path,
			StatusCode: resp.StatusCode,
			Message:    strings.TrimSpace(string(b)),
		}
		// 尽力解析出 message 字段，归因文本可读；解析失败保留原始截断体。
		var payload struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(b, &payload) == nil && payload.Message != "" {
			apiErr.Message = payload.Message
		}
		return apiErr
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("github: 解码 %s %s 响应: %w", method, path, err)
	}
	return nil
}
