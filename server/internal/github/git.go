package github

import (
	"context"
	"fmt"
	"os/exec"
)

// GitService 在本地 clone 里建分支、提交、推送。
type GitService struct{ Workdir string }

// WithWorkdir 返回一个设置了 Workdir 的副本。
func (g GitService) WithWorkdir(dir string) GitService { g.Workdir = dir; return g }

// CommitAndPush 在 Workdir 建（或切到）branch、提交全部改动、强推到 origin。
func (g GitService) CommitAndPush(ctx context.Context, branch, message string) error {
	run := func(args ...string) error {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", g.Workdir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %w: %s", args, err, string(out))
		}
		return nil
	}
	if err := run("checkout", "-B", branch); err != nil {
		return err
	}
	if err := run("add", "-A"); err != nil {
		return err
	}
	if err := run("commit", "-m", message, "--allow-empty"); err != nil {
		return err
	}
	return run("push", "-u", "origin", branch, "--force")
}
