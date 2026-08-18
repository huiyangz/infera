package persist

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/git"
)

// TestPersistSkipsPushWhenNoChanges：绑库交付无变更时跳过 push/PR——
// 分支内容与基线一致，push 是空操作、PR 必然 422（噪音）；
// 分支缺失正是父合并循环的「无变更子需求」跳过信号。
// diff 照常返回（驳回重做轮次里 HEAD 可能仍领先基线）。
func TestPersistSkipsPushWhenNoChanges(t *testing.T) {
	origin := newBare(t)
	g := git.New()
	ctx := context.Background()
	work := filepath.Join(t.TempDir(), "work")
	require.NoError(t, g.Clone(ctx, origin, "main", work))
	base, err := g.Head(ctx, work)
	require.NoError(t, err)

	p := NewLocal(g, "")
	res, err := p.Persist(ctx, Input{
		DeliveryID: testDeliveryID,
		RepoURL:    origin,
		BaseBranch: "main",
		BaseCommit: base,
		Workdir:    work,
		Title:      "无变更交付",
	})
	require.NoError(t, err, "无变更不是错误")
	require.Equal(t, "infera/11111111", res.Branch)
	require.Empty(t, res.PRURL)
	require.Empty(t, res.PRError)
	require.Empty(t, res.Diff, "基线==HEAD，无增量")
	require.Empty(t, remoteRef(t, origin, "refs/heads/infera/11111111"), "无变更不得推分支")

	// 有变更时照常推（对照组）。
	require.NoError(t, os.WriteFile(filepath.Join(work, "new.txt"), []byte("产出"), 0o644))
	res2, err := p.Persist(ctx, Input{
		DeliveryID: testDeliveryID,
		RepoURL:    origin,
		BaseBranch: "main",
		BaseCommit: base,
		Workdir:    work,
		Title:      "有变更交付",
	})
	require.NoError(t, err)
	require.Contains(t, res2.Diff, "+++ b/new.txt")
	require.Len(t, remoteRef(t, origin, "refs/heads/infera/11111111"), 40, "有变更必须推分支")

	// 第二轮无变更（驳回重做但 agent 没改东西）：远端分支保持第一轮内容，不再推。
	res3, err := p.Persist(ctx, Input{
		DeliveryID: testDeliveryID,
		RepoURL:    origin,
		BaseBranch: "main",
		BaseCommit: base,
		Workdir:    work,
		Title:      "重做无变更",
	})
	require.NoError(t, err)
	require.Contains(t, res3.Diff, "new.txt", "累计 diff 照常可读")
	require.Len(t, remoteRef(t, origin, "refs/heads/infera/11111111"), 40)
	tipAfterSkip := remoteRef(t, origin, "refs/heads/infera/11111111")

	// 合并循环形态：HEAD 因合并 commit 前进、workdir 干净（add -A 无新增）——
	// 必须照常推（"干净"不等于"无变更"）。
	require.NoError(t, os.WriteFile(filepath.Join(work, "merged.txt"), []byte("m"), 0o644))
	runGit(t, work, "add", "-A")
	runGit(t, work, "commit", "-m", "merge: child branch")
	res4, err := p.Persist(ctx, Input{
		DeliveryID: testDeliveryID,
		RepoURL:    origin,
		BaseBranch: "main",
		BaseCommit: base,
		Workdir:    work,
		Title:      "合并后固化",
	})
	require.NoError(t, err)
	newTip := remoteRef(t, origin, "refs/heads/infera/11111111")
	require.Len(t, newTip, 40, "HEAD 前进必须推分支")
	require.NotEqual(t, tipAfterSkip, newTip, "远端分支应更新到合并后内容")
	require.Contains(t, res4.Diff, "merged.txt")
}

// runGit 在指定目录跑 git（测试内直接造 commit，模拟合并循环产物）。
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}
