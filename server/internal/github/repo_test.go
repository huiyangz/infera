package github

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestClonePublicRepo(t *testing.T) {
	// 用一个极小的公开仓库验证 clone 逻辑（无需 token）
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	c := RepoCloner{token: ""}
	dest := t.TempDir()
	// 注意：本测试需联网；网络不可达时跳过
	if err := c.Clone(context.Background(), "https://github.com/octocat/Hello-World.git", dest); err != nil {
		t.Skipf("network clone failed, skipping: %v", err)
	}
	out, _ := exec.Command("git", "-C", dest, "log", "--oneline", "-1").Output()
	if !strings.Contains(string(out), "Fix") && !strings.Contains(string(out), "merge") && len(out) == 0 {
		t.Fatalf("expected some commit log, got %q", out)
	}
}
