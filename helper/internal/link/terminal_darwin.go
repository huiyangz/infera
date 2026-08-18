//go:build darwin

package link

import (
	"os/exec"
	"strings"
)

// OpenTerminal 在新的 Terminal.app 窗口里执行命令（macOS 经 osascript）。
func OpenTerminal(command string) error {
	return exec.Command("osascript", "-e", osascriptSource(command)).Run()
}

// osascriptSource 生成 AppleScript：激活 Terminal 并 do script。
// 命令内的反斜杠与双引号做 AppleScript 字符串转义（纯函数可单测）。
func osascriptSource(command string) string {
	esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return "tell application \"Terminal\"\n\tactivate\n\tdo script \"" + esc.Replace(command) + "\"\nend tell"
}
