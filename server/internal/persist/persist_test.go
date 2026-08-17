package persist

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/git"
)

// newBare 建一个带 1 个 commit（README.md）的本地 bare 仓库（模拟远端）。
func newBare(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin.git")
	work := filepath.Join(dir, "seed")
	run := func(cwd string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	run(dir, "init", "--bare", "-b", "main", origin)
	run(dir, "init", "-b", "main", work)
	_ = os.WriteFile(filepath.Join(work, "README.md"), []byte("# hi\n"), 0o644)
	run(work, "add", ".")
	run(work, "commit", "-m", "init")
	run(work, "push", origin, "main")
	return origin
}

// remoteRef 返回 origin 上 ref 的 commit（不存在返回空串）。
func remoteRef(t *testing.T, origin, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "ls-remote", origin, ref).Output()
	require.NoError(t, err)
	fields := strings.Fields(string(out))
	if len(fields) < 1 {
		return ""
	}
	return fields[0]
}

const testDeliveryID = "11111111-2222-3333-4444-555555555555"

// TestGreenfieldCommitAndDiff：无 .git 的目录（绿地 Acquire 产物）首次固化——
// 自动 init + commit，diff = 相对空树的全部内容；无远端所以不 push、无 PR。
func TestGreenfieldCommitAndDiff(t *testing.T) {
	work := t.TempDir() // 无 .git：模拟绿地 workdir
	require.NoError(t, os.WriteFile(filepath.Join(work, "a.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(work, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(work, "sub", "b.txt"), []byte("world"), 0o644))

	p := NewLocal(git.New(), "")
	res, err := p.Persist(context.Background(), Input{
		DeliveryID: testDeliveryID,
		Workdir:    work,
		Title:      "绿地交付",
	})
	require.NoError(t, err)
	require.Equal(t, "main", res.Branch)
	require.Empty(t, res.PRURL)
	require.Empty(t, res.PRError)
	require.Contains(t, res.Diff, "+++ b/a.txt")
	require.Contains(t, res.Diff, "+++ b/sub/b.txt")

	// 第二轮（驳回重做）：改文件再固化，diff 累积两轮内容。
	require.NoError(t, os.WriteFile(filepath.Join(work, "a.txt"), []byte("v2"), 0o644))
	res2, err := p.Persist(context.Background(), Input{DeliveryID: testDeliveryID, Workdir: work, Title: "绿地交付"})
	require.NoError(t, err)
	require.Contains(t, res2.Diff, "+v2")
	require.Contains(t, res2.Diff, "sub/b.txt", "Root diff 覆盖全部轮次的累计产出")
}

// TestRepoBackedPushAndDiff：绑库交付——diff 只含基线之后的增量，
// 分支推到远端；本地路径远端不开 PR。
func TestRepoBackedPushAndDiff(t *testing.T) {
	origin := newBare(t)
	g := git.New()
	ctx := context.Background()
	work := filepath.Join(t.TempDir(), "work")
	require.NoError(t, g.Clone(ctx, origin, "main", work))
	base, err := g.Head(ctx, work)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(work, "new.txt"), []byte("新产出"), 0o644))

	p := NewLocal(g, "")
	res, err := p.Persist(ctx, Input{
		DeliveryID: testDeliveryID,
		RepoURL:    origin,
		BaseBranch: "main",
		BaseCommit: base,
		Workdir:    work,
		Title:      "绑库交付",
	})
	require.NoError(t, err)
	require.Equal(t, "infera/11111111", res.Branch)
	require.Empty(t, res.PRURL, "本地路径远端不开 PR")
	require.Empty(t, res.PRError)
	require.Contains(t, res.Diff, "+++ b/new.txt")
	require.NotContains(t, res.Diff, "README.md", "diff 只含基线之后的增量")

	ref := remoteRef(t, origin, "refs/heads/infera/11111111")
	require.Len(t, ref, 40, "分支应已推到远端")
}

// TestForcePushSecondRound：驳回重做后再固化——同分支 force 覆盖，远端拿到第二轮内容。
func TestForcePushSecondRound(t *testing.T) {
	origin := newBare(t)
	g := git.New()
	ctx := context.Background()
	work := filepath.Join(t.TempDir(), "work")
	require.NoError(t, g.Clone(ctx, origin, "main", work))
	base, err := g.Head(ctx, work)
	require.NoError(t, err)
	in := Input{
		DeliveryID: testDeliveryID,
		RepoURL:    origin,
		BaseBranch: "main",
		BaseCommit: base,
		Workdir:    work,
		Title:      "两轮交付",
	}
	p := NewLocal(g, "")

	require.NoError(t, os.WriteFile(filepath.Join(work, "r1.txt"), []byte("round1"), 0o644))
	_, err = p.Persist(ctx, in)
	require.NoError(t, err)
	ref1 := remoteRef(t, origin, "refs/heads/infera/11111111")
	require.Len(t, ref1, 40)

	// 第二轮在全新的历史上重写产出（删掉 r1、新增 r2）→ 非快进，必须 force。
	require.NoError(t, os.Remove(filepath.Join(work, "r1.txt")))
	require.NoError(t, os.WriteFile(filepath.Join(work, "r2.txt"), []byte("round2"), 0o644))
	_, err = p.Persist(ctx, in)
	require.NoError(t, err)
	ref2 := remoteRef(t, origin, "refs/heads/infera/11111111")
	require.Len(t, ref2, 40)
	require.NotEqual(t, ref1, ref2, "force 推送应更新远端分支")

	// 远端分支内容确实是第二轮：clone 该分支验证。
	verify := filepath.Join(t.TempDir(), "verify")
	require.NoError(t, g.Clone(ctx, origin, "infera/11111111", verify))
	_, err = os.Stat(filepath.Join(verify, "r2.txt"))
	require.NoError(t, err, "远端分支应含第二轮产出")
	_, err = os.Stat(filepath.Join(verify, "r1.txt"))
	require.True(t, os.IsNotExist(err), "第一轮文件已被第二轮移除")
}

func TestGithubRepo(t *testing.T) {
	tests := []struct {
		rawURL string
		owner  string
		repo   string
		ok     bool
	}{
		{"https://github.com/acme/app.git", "acme", "app", true},
		{"https://github.com/acme/app", "acme", "app", true},
		{"https://gitlab.com/acme/app.git", "", "", false},
		{"https://github.enterprise.com/acme/app.git", "", "", false},
		{"/tmp/origin.git", "", "", false},
		{"https://github.com/acme", "", "", false},               // 只有 owner
		{"https://github.com/acme/app/tree/main", "", "", false}, // 多级路径
	}
	for _, tt := range tests {
		owner, repo, ok := githubRepo(tt.rawURL)
		require.Equal(t, tt.ok, ok, tt.rawURL)
		require.Equal(t, tt.owner, owner, tt.rawURL)
		require.Equal(t, tt.repo, repo, tt.rawURL)
	}
}

// TestCreatePR：201 → html_url；422（PR 已存在）→ PRURL 空 + PRError 有描述，不算失败。
func TestCreatePR(t *testing.T) {
	ctx := context.Background()

	var gotAuth, gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"html_url":"https://github.com/acme/app/pull/9"}`))
	}))
	defer ts.Close()

	l := NewLocal(git.New(), "ghp_testtoken")
	l.apiBase = ts.URL
	url, prErr := l.createPR(ctx, "acme", "app", "infera/11111111", "main", "标题")
	require.Empty(t, prErr)
	require.Equal(t, "https://github.com/acme/app/pull/9", url)
	require.Equal(t, "Bearer ghp_testtoken", gotAuth)
	var body map[string]string
	require.NoError(t, json.Unmarshal([]byte(gotBody), &body))
	require.Equal(t, map[string]string{
		"title": "标题", "head": "infera/11111111", "base": "main",
	}, body)

	// 失败路径：422 → 描述进 PRError，不返回 error。
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Validation Failed","errors":[{"message":"A pull request already exists"}]}`))
	}))
	defer ts2.Close()
	l.apiBase = ts2.URL
	url, prErr = l.createPR(ctx, "acme", "app", "infera/11111111", "main", "标题")
	require.Empty(t, url)
	require.Contains(t, prErr, "422")
	require.Contains(t, prErr, "already exists")
}

// TestPushFailureFails：push 失败整体失败（引擎据此 blocked 并保留 workdir）。
func TestPushFailureFails(t *testing.T) {
	g := git.New()
	ctx := context.Background()
	work := filepath.Join(t.TempDir(), "work")
	require.NoError(t, g.InitRepo(ctx, work))
	require.NoError(t, os.WriteFile(filepath.Join(work, "x.txt"), []byte("x"), 0o644))
	_, err := g.Commit(ctx, work, "base")
	require.NoError(t, err)
	base, err := g.Head(ctx, work)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(work, "y.txt"), []byte("y"), 0o644))

	// 不存在的远端路径：push 必失败（base 真实存在，diff 阶段能过）。
	p := NewLocal(g, "")
	_, err = p.Persist(ctx, Input{
		DeliveryID: testDeliveryID,
		RepoURL:    filepath.Join(t.TempDir(), "definitely-missing.git"),
		BaseBranch: "main",
		BaseCommit: base,
		Workdir:    work,
		Title:      "推不上去",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "git push")
}
