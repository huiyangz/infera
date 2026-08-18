// infera link：本机交互通道的 helper 守护进程（R4）。
// 常驻本机接收网页「在本地处理此阶段」按钮的指令，在交付 workdir 拉起
// 带 infera MCP 配置与初始提示的本机 CLI（claude/codex）。
// 安装/配置/使用说明见 helper/README.md。
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/tokfinity/infera/helper/internal/link"
)

func main() {
	cfg, err := link.Load(os.Args[1:], os.Getenv)
	if err != nil {
		log.Fatalf("infera-link: %v", err)
	}
	d := link.NewDaemon(cfg)
	log.Printf("infera-link: 监听 http://%s（server=%s cli=%s terminal=%s token=%s）",
		cfg.Listen, cfg.Server, cfg.CLI, cfg.Terminal, tokenState(cfg.Token))
	if err := http.ListenAndServe(cfg.Listen, d.Handler()); err != nil {
		log.Fatalf("infera-link: %v", err)
	}
}

func tokenState(tok string) string {
	if tok == "" {
		return "未设置（/handle 将报可操作错误）"
	}
	return "已设置"
}
