// Package git 封装 git 命令行。agent 无关、引擎无关的纯库。
// token 注入 https URL（GitHub PAT），本地路径原样使用（测试/自托管）。
package git

import (
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

type Git struct{ Token string }

func New() *Git { return &Git{} }

func injectToken(rawURL, token string) string {
	if token == "" || !strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.User = url.UserPassword(token, "")
	return u.String()
}

func (g *Git) run(cwd string, args ...string) (string, error) {
	full := append([]string{"-c", "user.email=agent@infera.dev", "-c", "user.name=infera-agent"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", args[0], err, out)
	}
	return string(out), nil
}

// LsRemote 毫秒级可达性/权限校验（不落盘）。
func (g *Git) LsRemote(rawURL string) error {
	cmd := exec.Command("git", "ls-remote", "--heads", injectToken(rawURL, g.Token))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ls-remote: %w: %s", err, out)
	}
	return nil
}

// Clone 浅克隆指定分支。
func (g *Git) Clone(rawURL, branch, dir string) error {
	_, err := g.run("", "clone", "--depth", "1", "--branch", branch, injectToken(rawURL, g.Token), dir)
	return err
}

// Head 返回当前 HEAD commit（快照基准）。
func (g *Git) Head(dir string) (string, error) {
	out, err := g.run(dir, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

// CommitAndPush 提交 workdir 全部变更并推到远端分支；无变更时返回 (false, nil)。
func (g *Git) CommitAndPush(dir, msg, ref, pushURL string) (bool, error) {
	if _, err := g.run(dir, "add", "-A"); err != nil {
		return false, err
	}
	st, err := g.run(dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(st) == "" {
		return false, nil
	}
	if _, err := g.run(dir, "commit", "-m", msg); err != nil {
		return false, err
	}
	if _, err := g.run(dir, "push", injectToken(pushURL, g.Token), "HEAD:"+ref); err != nil {
		return false, err
	}
	return true, nil
}
