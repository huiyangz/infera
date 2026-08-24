//go:build darwin

package link

import (
	"strings"
	"testing"
)

// TestOsascriptSource 只在 darwin 下编译：被测的 osascriptSource 本身就带
// //go:build darwin（terminal_darwin.go），此前混在无标签的 launch_test.go 里，
// 导致 Linux 上测试包编译失败（undefined: osascriptSource）。
func TestOsascriptSource(t *testing.T) {
	src := osascriptSource(`cd '/tmp/wd' && claude 'x'`)
	for _, want := range []string{"tell application \"Terminal\"", `do script "cd '/tmp/wd' && claude 'x'"`} {
		if !strings.Contains(src, want) {
			t.Errorf("osascript 源缺少 %q: %s", want, src)
		}
	}
	if strings.Count(src, "\"")%2 != 0 {
		t.Errorf("AppleScript 字符串引数应配对: %s", src)
	}
	// 命令含双引号/反斜杠时做 AppleScript 转义，不破坏外层字符串
	escaped := osascriptSource(`echo "hi\n"`)
	if !strings.Contains(escaped, `do script "echo \"hi\\n\""`) {
		t.Errorf("双引号与反斜杠应转义: %s", escaped)
	}
}
