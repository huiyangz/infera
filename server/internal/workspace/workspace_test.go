package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/git"
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

func TestAcquireClonesAndRecordsBase(t *testing.T) {
	origin := newBare(t)
	ws := New(t.TempDir(), git.New(), 30*time.Minute) // 测试注入无 token 的 git 实例

	dir, base, err := ws.Acquire(context.Background(), "d1", origin, "main")
	require.NoError(t, err)
	require.NotEmpty(t, base)
	require.FileExists(t, filepath.Join(dir, "README.md"))

	// 幂等：再次 Acquire 复用，不重新 clone
	dir2, base2, err := ws.Acquire(context.Background(), "d1", origin, "main")
	require.NoError(t, err)
	require.Equal(t, dir, dir2)
	require.Equal(t, base, base2)
}

func TestAcquireGreenfield(t *testing.T) {
	ws := New(t.TempDir(), git.New(), time.Hour)
	dir, base, err := ws.Acquire(context.Background(), "d2", "", "main")
	require.NoError(t, err)
	require.Empty(t, base) // 绿地：无仓库，base 为空
	require.DirExists(t, dir)
	require.DirExists(t, ws.Path("d2"))
}

func TestAcquireCloneFailureCleansUp(t *testing.T) {
	ws := New(t.TempDir(), git.New(), time.Hour)
	_, _, err := ws.Acquire(context.Background(), "bad", filepath.Join(t.TempDir(), "nonexistent.git"), "main")
	require.Error(t, err)
	// 失败后不残留半成品目录，且可重试
	dir, _, err := ws.Acquire(context.Background(), "bad", newBare(t), "main")
	require.NoError(t, err)
	require.DirExists(t, dir)
}

func TestReleaseCleansAfterRetention(t *testing.T) {
	ws := New(t.TempDir(), git.New(), 10*time.Millisecond)
	dir, _, err := ws.Acquire(context.Background(), "d3", newBare(t), "main")
	require.NoError(t, err)
	ws.Release("d3")
	require.Eventually(t, func() bool { _, err := os.Stat(dir); return os.IsNotExist(err) },
		2*time.Second, 5*time.Millisecond)
}

func TestReleaseImmediateWhenNoRetention(t *testing.T) {
	ws := New(t.TempDir(), git.New(), 0)
	dir, _, err := ws.Acquire(context.Background(), "d4", newBare(t), "main")
	require.NoError(t, err)
	ws.Release("d4")
	_, statErr := os.Stat(dir)
	require.True(t, os.IsNotExist(statErr))
}
