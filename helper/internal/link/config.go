// Package link 是本机交互通道的 helper（R4）：`infera link` 守护进程。
// 它常驻本机、监听 127.0.0.1，接收网页「在本地处理此阶段」按钮的指令，
// 经 infera MCP 服务（R3，契约见 docs/mcp.md）拉取交付上下文，
// 生成本机 CLI（claude/codex）的 MCP 客户端配置与初始提示，
// 在交付 workdir 拉起终端完成交互阶段，产出由 CLI 经 submit_stage_output 交回。
//
// 独立 Go module 且只用标准库：helper 跑在用户本机而非服务端，与 server 的
// 部署面解耦；`cd helper && go build ./cmd/infera-link` 即得单文件可执行。
package link

import (
	"flag"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Config 守护进程配置。解析优先级：flag > 环境变量 > 默认值。
type Config struct {
	Server   string // infera 服务基址（HTTP API 与 /mcp 同进程同端口）
	Token    string // INFERA_MCP_TOKEN（空 = 允许启动，/handle 时给可操作错误）
	Listen   string // 守护进程监听地址（只绑本机回环）
	CLI      string // 拉起的本机 CLI：claude | codex
	Terminal string // 拉起方式：auto（新终端窗口）| none（打印命令不执行，调试/无头）
}

// Load 解析配置。args 为命令行参数（不含程序名）；env 为环境变量读取函数。
func Load(args []string, env func(string) string) (Config, error) {
	if env == nil {
		env = func(string) string { return "" }
	}
	cfg := Config{
		Server:   "http://localhost:8080",
		Listen:   "127.0.0.1:8788",
		CLI:      "claude",
		Terminal: "auto",
	}
	fs := flag.NewFlagSet("infera-link", flag.ContinueOnError)
	fs.SetOutput(nil) // 静默：错误由返回值承载
	fs.StringVar(&cfg.Server, "server", getenv(env, "INFERA_URL", cfg.Server), "infera 服务基址")
	fs.StringVar(&cfg.Token, "token", getenv(env, "INFERA_MCP_TOKEN", cfg.Token), "INFERA_MCP_TOKEN（MCP 服务专用 token）")
	fs.StringVar(&cfg.Listen, "listen", getenv(env, "INFERA_LINK_ADDR", cfg.Listen), "监听地址（本机回环）")
	fs.StringVar(&cfg.CLI, "cli", getenv(env, "INFERA_LINK_CLI", cfg.CLI), "拉起的本机 CLI：claude | codex")
	fs.StringVar(&cfg.Terminal, "terminal", getenv(env, "INFERA_LINK_TERM", cfg.Terminal), "auto=新终端窗口；none=打印命令不执行")
	if err := fs.Parse(args); err != nil {
		return Config{}, fmt.Errorf("参数不合法: %w", err)
	}
	cfg.Server = strings.TrimRight(cfg.Server, "/")
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func getenv(env func(string) string, key, def string) string {
	if v := env(key); v != "" {
		return v
	}
	return def
}

func (c *Config) validate() error {
	if c.CLI != "claude" && c.CLI != "codex" {
		return fmt.Errorf("--cli 只支持 claude | codex，得到 %q", c.CLI)
	}
	if c.Terminal != "auto" && c.Terminal != "none" {
		return fmt.Errorf("--terminal 只支持 auto | none，得到 %q", c.Terminal)
	}
	u, err := url.Parse(c.Server)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("--server 必须是 http(s) 基址（如 http://localhost:8080），得到 %q", c.Server)
	}
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("--listen 必须是 host:port，得到 %q", c.Listen)
	}
	return nil
}

// MCPEndpoint MCP 服务端点：与 HTTP API 同进程同端口的 /mcp。
func (c Config) MCPEndpoint() string {
	return strings.TrimRight(c.Server, "/") + "/mcp"
}
