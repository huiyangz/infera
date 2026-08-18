//go:build !darwin

package link

import "fmt"

// OpenTerminal 非 macOS 平台暂未实现：用 --terminal=none 拿到命令自行粘贴。
func OpenTerminal(command string) error {
	return fmt.Errorf("当前平台暂不支持自动拉起终端，请用 --terminal=none 取命令手动执行")
}
