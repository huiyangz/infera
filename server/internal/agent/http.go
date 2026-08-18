package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// httpTimeout 单次 HTTP agent 调用的时间上限（LLM 后端可能较慢）。
const httpTimeout = 10 * time.Minute

// maxErrBody 错误信息里回显响应体的截断上限，避免日志/payload 爆炸。
const maxErrBody = 512

// HTTPRunner 把 agent 请求转发给远端 HTTP 服务：
// POST config.url {"role","prompt","workdir"} → 200 {"output":"..."}。
//
// workdir 契约：发送的是服务端本地绝对路径（与 cli/docker runner 的 Workdir 同源）。
// 这意味着远端服务必须能访问同一路径下的同一 workdir——部署上要求共享卷或同机；
// 若远端只能理解自己的相对标识，应由远端服务自行维护「绝对路径 ↔ 自有标识」映射。
type HTTPRunner struct {
	url string
	hc  *http.Client
}

func NewHTTP(url string) *HTTPRunner {
	return &HTTPRunner{url: url, hc: &http.Client{Timeout: httpTimeout}}
}

func (h *HTTPRunner) Run(ctx context.Context, req Request) (Result, error) {
	body, err := json.Marshal(map[string]string{
		"role":    req.Role,
		"prompt":  req.Prompt,
		"workdir": req.Workdir,
	})
	if err != nil {
		return Result{}, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := h.hc.Do(hreq)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBody))
		return Result{}, fmt.Errorf("http runner: %s 返回 %d: %s", h.url, resp.StatusCode, string(snippet))
	}
	var out struct {
		Output string `json:"output"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Result{}, fmt.Errorf("http runner: 解析响应失败: %w", err)
	}
	return Result{Output: out.Output}, nil
}

var _ Runner = (*HTTPRunner)(nil)
