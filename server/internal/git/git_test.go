package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	ctx := context.Background()
	require.NoError(t, g.LsRemote(ctx, origin))
	require.Error(t, g.LsRemote(ctx, filepath.Join(origin, "nope")))

	cloneDir := filepath.Join(t.TempDir(), "work")
	require.NoError(t, g.Clone(ctx, origin, "main", cloneDir))
	head, err := g.Head(ctx, cloneDir)
	require.NoError(t, err)
	require.Len(t, head, 40)
}

func TestCommitAndPush(t *testing.T) {
	origin := newBare(t)
	g := New()
	ctx := context.Background()
	cloneDir := filepath.Join(t.TempDir(), "work")
	require.NoError(t, g.Clone(ctx, origin, "main", cloneDir))
	require.NoError(t, os.WriteFile(filepath.Join(cloneDir, "f.txt"), []byte("x"), 0o644))
	pushed, err := g.CommitAndPush(ctx, cloneDir, "feat: add file", "refs/heads/infera/test", origin)
	require.NoError(t, err)
	require.True(t, pushed)

	// 无变更时返回 (false, nil)
	pushed, err = g.CommitAndPush(ctx, cloneDir, "empty", "refs/heads/infera/test2", origin)
	require.NoError(t, err)
	require.False(t, pushed)
}

func TestInjectToken(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
		token  string
		want   string
	}{
		{
			name:   "https + token 注入 userinfo",
			rawURL: "https://github.com/acme/app.git",
			token:  "ghp_secret123",
			want:   "https://ghp_secret123:@github.com/acme/app.git",
		},
		{
			name:   "空 token 原样返回",
			rawURL: "https://github.com/acme/app.git",
			token:  "",
			want:   "https://github.com/acme/app.git",
		},
		{
			name:   "http（非 https）不注入",
			rawURL: "http://github.com/acme/app.git",
			token:  "ghp_secret123",
			want:   "http://github.com/acme/app.git",
		},
		{
			name:   "本地路径不注入",
			rawURL: "/tmp/origin.git",
			token:  "ghp_secret123",
			want:   "/tmp/origin.git",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, injectToken(tt.rawURL, tt.token))
		})
	}
}

// TestCloneDoesNotPersistToken：Clone 后 origin 存的必须是 rawURL——
// token 不得写进 workdir 的 .git/config。本地 bare 路径不走注入，
// 此处验证 set-url 生效（remote.origin.url == rawURL，即使 token 非空）；
// https URL 的注入行为由 TestInjectToken 覆盖。
func TestCloneDoesNotPersistToken(t *testing.T) {
	origin := newBare(t)
	g := &Git{Token: "ghp_faketoken"}
	cloneDir := filepath.Join(t.TempDir(), "work")
	require.NoError(t, g.Clone(context.Background(), origin, "main", cloneDir))

	out, err := exec.Command("git", "-C", cloneDir, "config", "--get", "remote.origin.url").Output()
	require.NoError(t, err)
	require.Equal(t, origin, strings.TrimSpace(string(out)))

	// 双保险：整个 .git/config 里不能出现 token。
	cfg, err := os.ReadFile(filepath.Join(cloneDir, ".git", "config"))
	require.NoError(t, err)
	require.NotContains(t, string(cfg), g.Token)
}

// TestRedact：token 及其 URL 转义形态都必须被抹掉。
func TestRedact(t *testing.T) {
	g := &Git{Token: "ghp_secret123"}
	require.Equal(t,
		"fatal: unable to access 'https://***:@github.com/acme/app.git/'",
		g.redact("fatal: unable to access 'https://ghp_secret123:@github.com/acme/app.git/'"))
	require.Equal(t, "no token here", g.redact("no token here"))
	require.Equal(t, "raw", (&Git{}).redact("raw"))
}

// TestLsRemoteRedactsToken：真实失败路径上，错误信息不得包含 token。
// 127.0.0.1:1 无监听，连接立刻被拒，不需要网络。
func TestLsRemoteRedactsToken(t *testing.T) {
	g := &Git{Token: "ghp_supersecret99"}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err := g.LsRemote(ctx, "https://127.0.0.1:1/acme/app.git")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "ghp_supersecret99")
	require.Contains(t, err.Error(), "ls-remote")
}
