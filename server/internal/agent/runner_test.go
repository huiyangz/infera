package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildPrompt(t *testing.T) {
	// code_gen 模板同时含 {description} 与 {spec}：空 spec 渲染为（无），无占位符残留。
	// （spec 角色只消费 {description}，无 {spec} 可替换，故不能用于验证空值逻辑。）
	p := BuildPrompt("code_gen", "写 README", "", "")
	require.Contains(t, p, "写 README")
	require.Contains(t, p, "（无）")
	require.NotContains(t, p, "{description}")
	require.NotContains(t, p, "{spec}")
	require.NotContains(t, p, "{feedback}") // 无反馈：反馈行整行省略
	require.NotContains(t, p, "上一轮反馈")

	p = BuildPrompt("spec", "写 README", "", "")
	require.Contains(t, p, "写 README")
	require.NotContains(t, p, "{description}")
	require.NotContains(t, p, "上一轮反馈")

	p = BuildPrompt("test_gen", "写 README", "SPEC 正文", "")
	require.Contains(t, p, "SPEC 正文")
	require.NotContains(t, p, "{spec}")

	// code_review 模板含 {spec}：审查需对照规格。
	p = BuildPrompt("code_review", "写 README", "SPEC 正文", "")
	require.Contains(t, p, "SPEC 正文")
	require.NotContains(t, p, "{spec}")
}

func TestBuildPromptFeedback(t *testing.T) {
	// 反馈非空：spec / code_gen 的反馈行渲染实际内容。
	p := BuildPrompt("spec", "写 README", "", "人打回：验收标准缺失")
	require.Contains(t, p, "上一轮反馈：人打回：验收标准缺失")
	require.NotContains(t, p, "{feedback}")

	p = BuildPrompt("code_gen", "写 README", "SPEC 正文", "上一轮 unit_test 未过：FAIL")
	require.Contains(t, p, "上一轮反馈：上一轮 unit_test 未过：FAIL")
	require.Contains(t, p, "SPEC 正文")
	require.NotContains(t, p, "{feedback}")

	// 不含反馈行的模板传 feedback 不改变输出（无占位符可替换）。
	p = BuildPrompt("test_gen", "写 README", "SPEC 正文", "无关反馈")
	require.Equal(t, BuildPrompt("test_gen", "写 README", "SPEC 正文", ""), p)
}

func TestLocalRunner(t *testing.T) {
	dir := t.TempDir()
	r := NewLocal([]string{"sh", "-c", "echo done: $INFERA_ROLE; echo ran > $INFERA_WORKDIR/agent_ran.txt"})
	res, err := r.Run(context.Background(), Request{Role: "spec", Prompt: "p", Workdir: dir})
	require.NoError(t, err)
	require.Contains(t, res.Output, "done: spec")
	require.FileExists(t, filepath.Join(dir, "agent_ran.txt"))
}

func TestLocalRunnerPromptViaEnvAndStdin(t *testing.T) {
	dir := t.TempDir()
	// stdin 收 prompt，环境变量透传 INFERA_PROMPT
	r := NewLocal([]string{"sh", "-c", "cat > in.txt; test \"$INFERA_PROMPT\" = hello && echo env-ok"})
	res, err := r.Run(context.Background(), Request{Role: "code_gen", Prompt: "hello", Workdir: dir})
	require.NoError(t, err)
	require.Contains(t, res.Output, "env-ok")
	b, _ := os.ReadFile(filepath.Join(dir, "in.txt"))
	require.Equal(t, "hello", string(b))
}
