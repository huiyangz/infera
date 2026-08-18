package link

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestDaemon 组一个全注入的 daemon：fakeMCP + 捕获拉起命令 + 临时暂存目录。
func newTestDaemon(t *testing.T, f *fakeMCP) (*Daemon, *[]string, string) {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	cfg := Config{Server: srv.URL, Token: "sekret", CLI: "claude", Listen: "127.0.0.1:8788", Terminal: "auto"}
	d := NewDaemon(cfg)
	d.mc = &Client{Endpoint: cfg.MCPEndpoint(), Token: cfg.Token}
	var cmds []string
	d.Launch = func(cmd string) error { cmds = append(cmds, cmd); return nil }
	root := t.TempDir()
	d.StageDir = func(string) (string, error) { return root, nil }
	return d, &cmds, root
}

func post(d *Daemon, path, origin, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)
	return w
}

func TestHealthz(t *testing.T) {
	f := &fakeMCP{token: "sekret", toolText: sampleContext}
	d, _, _ := newTestDaemon(t, f)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("healthz 应 200，得到 %d", w.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("healthz 应为 JSON: %v", err)
	}
	if got["ok"] != true || got["cli"] != "claude" || got["token_set"] != true {
		t.Errorf("healthz 内容不符: %v", got)
	}
	if strings.Contains(w.Body.String(), "sekret") {
		t.Errorf("healthz 不得泄漏 token")
	}
}

func TestHandleHappyPath(t *testing.T) {
	f := &fakeMCP{token: "sekret", toolText: sampleContext}
	d, cmds, root := newTestDaemon(t, f)
	w := post(d, "/handle", "http://localhost:5173", `{"delivery_id":"d-1"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("handle 应 200，得到 %d: %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应应为 JSON: %v", err)
	}
	if got["ok"] != true || got["node"] != "spec" || got["workdir"] != "/tmp/wd" {
		t.Errorf("响应内容不符: %v", got)
	}
	if len(*cmds) != 1 || !strings.Contains((*cmds)[0], "--mcp-config '"+root+"/mcp.json'") {
		t.Errorf("应拉起引用暂存配置的命令: %v", *cmds)
	}
	// 暂存文件：配置带端点与 token，提示含交回指引；配置文件权限收紧
	cfgB, err := os.ReadFile(filepath.Join(root, "mcp.json"))
	if err != nil || !strings.Contains(string(cfgB), "/mcp") || !strings.Contains(string(cfgB), "sekret") {
		t.Errorf("mcp.json 落盘内容不符: %q %v", cfgB, err)
	}
	promptB, err := os.ReadFile(filepath.Join(root, "prompt.txt"))
	if err != nil || !strings.Contains(string(promptB), "submit_stage_output") {
		t.Errorf("prompt.txt 落盘内容不符: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(root, "mcp.json")); err != nil || fi.Mode().Perm() != 0600 {
		t.Errorf("mcp.json 应 0600（内含 token）: %v %v", fi, err)
	}
	// localhost 来源回显 CORS
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Errorf("应回显 localhost Origin")
	}
}

func TestHandlePreflight(t *testing.T) {
	f := &fakeMCP{token: "sekret", toolText: sampleContext}
	d, _, _ := newTestDaemon(t, f)
	req := httptest.NewRequest(http.MethodOptions, "/handle", nil)
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("预检应 204，得到 %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "http://127.0.0.1:5173" ||
		!strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "Content-Type") {
		t.Errorf("预检 CORS 头不符: %v", w.Header())
	}
}

func TestHandleForeignOriginNoCORS(t *testing.T) {
	f := &fakeMCP{token: "sekret", toolText: sampleContext}
	d, _, _ := newTestDaemon(t, f)
	w := post(d, "/handle", "http://evil.example.com", `{"delivery_id":"d-1"}`)
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("非本机 Origin 不应给 CORS 头")
	}
}

func TestHandleMethodNotAllowed(t *testing.T) {
	f := &fakeMCP{token: "sekret", toolText: sampleContext}
	d, _, _ := newTestDaemon(t, f)
	req := httptest.NewRequest(http.MethodGet, "/handle", nil)
	w := httptest.NewRecorder()
	d.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /handle 应 405，得到 %d", w.Code)
	}
}

func TestHandleBadRequests(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"空 delivery_id", `{"delivery_id":""}`, "delivery_id"},
		{"坏 JSON", `{`, "请求体"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f2 := &fakeMCP{token: "sekret", toolText: sampleContext}
			d, cmds, _ := newTestDaemon(t, f2)
			w := post(d, "/handle", "", c.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("应 400，得到 %d", w.Code)
			}
			if !strings.Contains(w.Body.String(), c.want) {
				t.Errorf("错误应可操作（含 %q）: %s", c.want, w.Body.String())
			}
			if len(*cmds) != 0 || len(f2.methods) != 0 {
				t.Errorf("不应触达 MCP/拉起")
			}
		})
	}
}

func TestHandleNoTokenConfigured(t *testing.T) {
	f := &fakeMCP{token: "sekret", toolText: sampleContext}
	d, cmds, _ := newTestDaemon(t, f)
	d.cfg.Token = ""
	w := post(d, "/handle", "", `{"delivery_id":"d-1"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "INFERA_MCP_TOKEN") {
		t.Fatalf("缺 token 应 400 且提示设置 token，得到 %d: %s", w.Code, w.Body.String())
	}
	if len(*cmds) != 0 {
		t.Errorf("不应拉起")
	}
}

func TestHandleMCPErrorPropagates(t *testing.T) {
	f := &fakeMCP{token: "sekret", toolErr: "交付不存在"}
	d, cmds, _ := newTestDaemon(t, f)
	w := post(d, "/handle", "", `{"delivery_id":"d-nope"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "交付不存在") {
		t.Fatalf("MCP 工具错误应透传，得到 %d: %s", w.Code, w.Body.String())
	}
	if len(*cmds) != 0 {
		t.Errorf("不应拉起")
	}
}

func TestHandleTerminalNoneLogsCommand(t *testing.T) {
	f := &fakeMCP{token: "sekret", toolText: sampleContext}
	d, _, _ := newTestDaemon(t, f)
	d.cfg.Terminal = "none"
	d.Launch = nil // 走真实默认拉起路径（none 分支）
	var logged strings.Builder
	d.log = log.New(&logged, "", 0)
	w := post(d, "/handle", "", `{"delivery_id":"d-1"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("none 模式应 200，得到 %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(logged.String(), "claude --mcp-config") {
		t.Errorf("none 模式应把命令打印到守护进程日志: %q", logged.String())
	}
}

func TestDefaultStageDir(t *testing.T) {
	dir, err := defaultStageDir("d-1")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(dir), ".infera/link/d-1") {
		t.Errorf("暂存目录应为 ~/.infera/link/<delivery_id>，得到 %q", dir)
	}
}

var _ = context.Background
