package testrunner

import (
	"context"
	"strings"
)

// CmdRunner 是 RealRunner 依赖的"在 workdir 起容器跑命令"能力（由 DockerBackend.RunCommand 提供）。
type CmdRunner interface {
	RunCommand(ctx context.Context, cmd []string, workdir string) (stdout string, exitCode int, err error)
}

// RealRunner 在容器里跑 go test，按 exit code 判定。
type RealRunner struct {
	cmd     CmdRunner
	workdir string // 本地 clone 路径，挂载进容器 /work
}

func NewRealRunner(cmd CmdRunner, workdir string) *RealRunner {
	return &RealRunner{cmd: cmd, workdir: workdir}
}

func (r *RealRunner) Run(ctx context.Context, workdir string) (Result, error) {
	wd := workdir
	if wd == "" {
		wd = r.workdir
	}
	out, code, err := r.cmd.RunCommand(ctx,
		[]string{"sh", "-c", "cd /work && go test ./... 2>&1"}, wd)
	if err != nil {
		return Result{Pass: false, Detail: "run error: " + err.Error()}, err
	}
	if code == 0 {
		return Result{Pass: true, Detail: out}, nil
	}
	// 截取失败摘要（最后 10 行）
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > 10 {
		lines = lines[len(lines)-10:]
	}
	return Result{Pass: false, Detail: strings.Join(lines, "\n")}, nil
}
