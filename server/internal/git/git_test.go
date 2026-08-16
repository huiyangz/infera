package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// newBare 建一个带 1 个 commit 的本地 bare 仓库（模拟远端）。
func newBare(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin.git")
	work := filepath.Join(dir, "seed")
	run := func(cwd string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return string(out)
	}
	run(dir, "init", "--bare", "-b", "main", origin)
	run(dir, "init", "-b", "main", work)
	_ = os.WriteFile(filepath.Join(work, "README.md"), []byte("# hi\n"), 0o644)
	run(work, "add", ".")
	run(work, "commit", "-m", "init")
	run(work, "push", origin, "main")
	return origin
}

func TestLsRemoteAndCloneAndHead(t *testing.T) {
	origin := newBare(t)
	g := New()
	require.NoError(t, g.LsRemote(origin))
	require.Error(t, g.LsRemote(filepath.Join(origin, "nope")))

	cloneDir := filepath.Join(t.TempDir(), "work")
	require.NoError(t, g.Clone(origin, "main", cloneDir))
	head, err := g.Head(cloneDir)
	require.NoError(t, err)
	require.Len(t, head, 40)
}

func TestCommitAndPush(t *testing.T) {
	origin := newBare(t)
	g := New()
	cloneDir := filepath.Join(t.TempDir(), "work")
	require.NoError(t, g.Clone(origin, "main", cloneDir))
	require.NoError(t, os.WriteFile(filepath.Join(cloneDir, "f.txt"), []byte("x"), 0o644))
	pushed, err := g.CommitAndPush(cloneDir, "feat: add file", "refs/heads/infera/test", origin)
	require.NoError(t, err)
	require.True(t, pushed)

	// 无变更时返回 (false, nil)
	pushed, err = g.CommitAndPush(cloneDir, "empty", "refs/heads/infera/test2", origin)
	require.NoError(t, err)
	require.False(t, pushed)
}
