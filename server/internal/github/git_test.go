package github

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func initBareRemote(t *testing.T) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	_ = exec.Command("git", "init", "--bare", remote).Run()
	return remote
}

func initLocalClone(t *testing.T, remote string) string {
	t.Helper()
	work := filepath.Join(t.TempDir(), "work")
	_ = exec.Command("git", "clone", remote, work).Run()
	for _, args := range [][]string{
		{"-C", work, "config", "user.email", "t@t"},
		{"-C", work, "config", "user.name", "t"},
		{"-C", work, "commit", "--allow-empty", "-m", "init"},
		{"-C", work, "push", "origin", "HEAD"},
	} {
		_ = exec.Command("git", args...).Run()
	}
	return work
}

func TestCommitAndPushCreatesBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	remote := initBareRemote(t)
	work := initLocalClone(t, remote)
	writeFile(t, filepath.Join(work, "a.txt"), "hi")

	g := GitService{Workdir: work}
	err := g.CommitAndPush(context.Background(), "feat-1", "add a")
	assert.NoError(t, err)

	out, _ := exec.Command("git", "ls-remote", "--heads", remote, "feat-1").Output()
	assert.Contains(t, string(out), "feat-1")
}
