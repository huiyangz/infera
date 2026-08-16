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
	p := BuildPrompt("code_gen", "写 README", "")
	require.Contains(t, p, "写 README")
	require.Contains(t, p, "（无）")
	require.NotContains(t, p, "{description}")
	require.NotContains(t, p, "{spec}")

	p = BuildPrompt("spec", "写 README", "")
	require.Contains(t, p, "写 README")
	require.NotContains(t, p, "{description}")

	p = BuildPrompt("test_gen", "写 README", "SPEC 正文")
	require.Contains(t, p, "SPEC 正文")
	require.NotContains(t, p, "{spec}")
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
