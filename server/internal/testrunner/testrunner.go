// Package testrunner 执行 unit_test 命令节点。
package testrunner

import (
	"context"
	"os/exec"

	"github.com/tokfinity/infera/internal/agent"
)

// Local 在主机执行 shell 脚本（cfg.TestCmd），退出码 0 = 通过。
type Local struct{ Script string }

func (l *Local) RunTests(_ context.Context, workdir string) (bool, string, error) {
	cmd := exec.Command("sh", "-c", l.Script)
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()
	return err == nil, string(out), nil
}

// Docker 在 agent 容器里跑命令（bind workdir → /work）。
type Docker struct {
	Image string
	Cmd   []string
}

func (d *Docker) RunTests(ctx context.Context, workdir string) (bool, string, error) {
	out, err := agent.RunInContainer(ctx, d.Image, d.Cmd, workdir)
	return err == nil, out, nil
}
