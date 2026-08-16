package agent

import (
	"bytes"
	"context"
	"os/exec"
)

// LocalRunner 在主机上执行命令（E2E 与本地 claude/pi CLI 调试）。
// 契约环境变量：INFERA_ROLE / INFERA_WORKDIR / INFERA_PROMPT；prompt 同时写 stdin。
type LocalRunner struct{ cmd []string }

func NewLocal(cmd []string) *LocalRunner { return &LocalRunner{cmd: cmd} }

func (l *LocalRunner) Run(ctx context.Context, req Request) (Result, error) {
	cmd := exec.CommandContext(ctx, l.cmd[0], l.cmd[1:]...)
	cmd.Dir = req.Workdir
	cmd.Env = append(cmd.Environ(),
		"INFERA_ROLE="+req.Role,
		"INFERA_WORKDIR="+req.Workdir,
		"INFERA_PROMPT="+req.Prompt,
	)
	cmd.Stdin = bytes.NewBufferString(req.Prompt)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return Result{Output: out.String()}, err
}
