// 守护进程：`infera link` 常驻本机（只绑回环），网页「在本地处理此阶段」按钮
// 直连它。POST /handle 是完整链路：MCP get_context 拉上下文 → 组装拉起计划
// （MCP 客户端配置 + 初始提示）落盘暂存目录 → 在交付 workdir 拉起新终端里的
// 本机 CLI。GET /healthz 供探测（不泄漏 token）。
//
// 为什么是「网页直连本机 daemon」而不是 URL scheme：daemon 方案无 OS 注册步骤、
// 天然支持 healthz 探测与错误回显（scheme 只能单向唤起、无法回报配置错误）；
// CORS 收紧为本机来源，与 server /mcp 的 Origin 纪律一致。
package link

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Daemon 本机守护进程。
type Daemon struct {
	cfg      Config
	mc       *Client
	Launch   func(command string) error              // 终端拉起覆盖（测试注入；nil=launchDefault）
	StageDir func(deliveryID string) (string, error) // 每次拉起的暂存目录（测试注入）
	log      *log.Logger
}

// NewDaemon 按配置建 daemon（终端拉起策略见 launchDefault，可被测试替换）。
func NewDaemon(cfg Config) *Daemon {
	return &Daemon{
		cfg:      cfg,
		mc:       &Client{Endpoint: cfg.MCPEndpoint(), Token: cfg.Token},
		StageDir: defaultStageDir,
		log:      log.New(os.Stderr, "", log.LstdFlags),
	}
}

// launch 拉起终端：注入的 Launch 优先（测试），否则走默认策略。
func (d *Daemon) launch(command string) error {
	if d.Launch != nil {
		return d.Launch(command)
	}
	return d.launchDefault(command)
}

// launchDefault 按当前配置拉起：auto=新终端窗口（OpenTerminal）；
// none=把命令打到守护进程日志（调试/无头/非 macOS 平台手工粘贴）。
// 运行期读 cfg.Terminal，切模式即生效。
func (d *Daemon) launchDefault(command string) error {
	if d.cfg.Terminal == "none" {
		d.log.Printf("拉起命令（--terminal=none，未执行）:\n%s", command)
		return nil
	}
	return OpenTerminal(command)
}

// defaultStageDir 每个交付一个暂存目录：~/.infera/link/<delivery_id>/
// （mcp 配置与初始提示落在这里；内含 token，目录权限 0700、文件 0600）。
func defaultStageDir(deliveryID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("定位用户主目录失败: %w", err)
	}
	return filepath.Join(home, ".infera", "link", deliveryID), nil
}

// Handler 返回守护进程 HTTP 路由（/healthz、/handle）。
func (d *Daemon) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", d.handleHealthz)
	mux.HandleFunc("/handle", d.handleHandle)
	return mux
}

func (d *Daemon) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 GET"})
		return
	}
	d.cors(w, r)
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"ok":        true,
		"server":    d.cfg.Server,
		"cli":       d.cfg.CLI,
		"terminal":  d.cfg.Terminal,
		"token_set": d.cfg.Token != "",
	})
}

// handleHandle 「在本地处理此阶段」：body {"delivery_id": "..."}。
// Origin 强制白名单（无 Origin / 非本机一律 403）：/handle 会驱动守护进程
// 拉起终端执行 CLI，恶意网页可用 no-cors text/plain 简单请求直打本机——
// CORS 头只挡「读响应」不挡「发请求」。/handle 只服务网页按钮（浏览器必带
// Origin），非浏览器自动化走 MCP 直连，无 Origin 也拒。
// 其余错误一律 JSON + 可操作文本：配置问题（400）、infera 服务问题（400 透传
// 服务端文本）、本机拉起问题（500）。
func (d *Daemon) handleHandle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions { // CORS 预检（仅本机来源回显 CORS 头）
		d.cors(w, r)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
		return
	}
	if !originAllowed(r) {
		writeJSONStatus(w, http.StatusForbidden, map[string]string{"error": "Origin 不被允许（仅接受本机网页来源）"})
		return
	}
	d.cors(w, r)
	var body struct {
		DeliveryID string `json:"delivery_id"`
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err := json.Unmarshal(raw, &body); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "请求体不合法（期望 {\"delivery_id\": \"...\"}）"})
		return
	}
	if strings.TrimSpace(body.DeliveryID) == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "缺少 delivery_id"})
		return
	}
	if !validDeliveryID(body.DeliveryID) {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "delivery_id 不合法（仅字母数字、-、_，最长 128）"})
		return
	}
	if d.cfg.Token == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{
			"error": "未配置 MCP token：启动 infera-link 时用 --token 或环境变量 INFERA_MCP_TOKEN 提供（与 infera 服务端同值）",
		})
		return
	}
	ctx := r.Context()
	mc, err := d.mc.GetContext(ctx, body.DeliveryID)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	stageDir, err := d.StageDir(body.DeliveryID)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	plan, err := Plan(mc, d.cfg, stageDir)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := d.stage(plan, stageDir); err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := d.launch(plan.Command); err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "拉起终端失败: " + err.Error()})
		return
	}
	d.log.Printf("handle: delivery=%s node=%s workdir=%s", body.DeliveryID, plan.Node, plan.Workdir)
	writeJSONStatus(w, http.StatusOK, map[string]any{
		"ok": true, "node": plan.Node, "workdir": plan.Workdir,
		"cli": d.cfg.CLI, "terminal": d.cfg.Terminal,
	})
}

// stage 落盘暂存文件（MCP 配置 + 初始提示）。目录 0700、文件 0600：配置内含 token。
func (d *Daemon) stage(plan *LaunchPlan, dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建暂存目录失败: %w", err)
	}
	cfgName := "mcp.json"
	if d.cfg.CLI == "codex" {
		cfgName = "mcp.toml"
	}
	if err := os.WriteFile(filepath.Join(dir, cfgName), []byte(plan.MCPConfig), 0o600); err != nil {
		return fmt.Errorf("写入 MCP 配置失败: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte(plan.Prompt), 0o600)
}

// originAllowed /handle 的来源白名单：必须带 Origin 且主机为本机
// （localhost/127.0.0.1/::1）。比 cors() 的回显判定更严——回显不给头
// 只挡「读响应」，不挡「发请求」；no-cors 简单请求照样能打到守护进程。
func originAllowed(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return false
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// validDeliveryID delivery_id 进路径（暂存目录 ~/.infera/link/<id>）前的校验：
// 白名单字符集（UUID/短横线命名都覆盖），挡路径穿越（../、分隔符）与怪值。
func validDeliveryID(id string) bool {
	if len(id) > 128 {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return id != ""
}

// cors 本机来源回显 Origin（与 server /mcp 的 Origin 纪律同款：仅 localhost）。
// /handle 已由 originAllowed 强制白名单；此处回显让正常网页按钮能读响应。
func (d *Daemon) cors(w http.ResponseWriter, r *http.Request) {
	o := r.Header.Get("Origin")
	if o == "" {
		return
	}
	u, err := url.Parse(o)
	if err != nil {
		return
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		w.Header().Set("Access-Control-Allow-Origin", o)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Max-Age", "600")
	}
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
