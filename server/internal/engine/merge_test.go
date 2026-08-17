package engine

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/git"
	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/workspace"
)

// newBareOrigin 建一个带 1 个 commit 的本地 bare 仓库（模拟远端，引擎测试用）。
func newBareOrigin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin.git")
	work := filepath.Join(dir, "seed")
	run := func(cwd string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	run(dir, "init", "--bare", "-b", "main", origin)
	run(dir, "init", "-b", "main", work)
	require.NoError(t, os.WriteFile(filepath.Join(work, "README.md"), []byte("# hi\n"), 0o644))
	run(work, "add", ".")
	run(work, "commit", "-m", "init")
	run(work, "push", origin, "main")
	return origin
}

// newRealEnv 真 git + 真 workspace 的引擎环境（agent/testRunner/persister 仍是替身）。
func newRealEnv(t *testing.T) (*Engine, *store.Memory, *store.Project) {
	t.Helper()
	origin := newBareOrigin(t)
	st := store.NewMemory()
	ctx := context.Background()
	proj := &store.Project{Name: "demo", RepoURL: origin, DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, proj))
	ws := workspace.New(t.TempDir(), git.New(), 0)
	e := New(st, &fakeRunner{}, ws, passTR{}).WithPersister(&fakePersister{})
	return e, st, proj
}

// pushChildBranch 模拟子需求固化：从 origin 克隆、写文件、推 infera/<子前8位> 分支。
func pushChildBranch(t *testing.T, proj *store.Project, childID, file, content string) {
	t.Helper()
	g := git.New()
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), childID[:8])
	require.NoError(t, g.Clone(ctx, proj.RepoURL, "main", dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644))
	pushed, err := g.CommitAndPush(ctx, dir, "feat: "+file, "refs/heads/infera/"+childID[:8], proj.RepoURL)
	require.NoError(t, err)
	require.True(t, pushed)
}

// completeChild 直接把子需求置为 completed（绕过完整流水线，聚焦合并逻辑）。
func completeChild(t *testing.T, st *store.Memory, childID string) {
	t.Helper()
	c, err := st.GetDelivery(context.Background(), childID)
	require.NoError(t, err)
	c.Status = StatusCompleted
	require.NoError(t, st.UpdateDelivery(context.Background(), c))
}

// mergedChildCount 数父的 durable 合并标记。
func mergedChildCount(t *testing.T, st *store.Memory, parentID string) int {
	t.Helper()
	arts, err := st.ListArtifacts(context.Background(), parentID)
	require.NoError(t, err)
	n := 0
	for _, a := range arts {
		if a.Kind == "merged_child" {
			n++
		}
	}
	return n
}

// eventPayload 取指定类型事件的 payload（最新一条）。
func eventPayload(t *testing.T, st *store.Memory, deliveryID, eventType string) map[string]any {
	t.Helper()
	evs, err := st.ListEvents(context.Background(), deliveryID)
	require.NoError(t, err)
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].EventType == eventType {
			var m map[string]any
			require.NoError(t, json.Unmarshal(evs[i].Payload, &m))
			return m
		}
	}
	t.Fatalf("event %s not found on %s", eventType, deliveryID)
	return nil
}

// waitUntil 轮询直到 cond 为真或超时（异步父推进的测试同步点）。
func waitUntil(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting: %s", msg)
}

// TestSplitIncrementalMergeWavesAndFinalize 拆分主链路（真 git）：
// 增量合并（完成一个合一个）→ wave 1 全合并后 wave 2 启动 → 全部合并后
// 父写摘要、推进 unit_test → code_review 门禁（含固化）。
func TestSplitIncrementalMergeWavesAndFinalize(t *testing.T) {
	e, st, proj := newRealEnv(t)
	ctx := context.Background()
	var started []string
	e.OnStartDelivery = func(id string) { started = append(started, id) }

	parent := &store.Delivery{ProjectID: proj.ID, Title: "父需求", Status: StatusActive, CurrentStage: "intake"}
	require.NoError(t, st.CreateDelivery(ctx, parent))
	require.NoError(t, e.Start(ctx, parent.ID))
	_, err := e.ApproveWithSplit(ctx, parent.ID, []store.ChildSpec{
		{Title: "子A", Wave: 1}, {Title: "子B", Wave: 1}, {Title: "子C", Wave: 2},
	})
	require.NoError(t, err)
	children, err := st.ListChildDeliveries(ctx, parent.ID)
	require.NoError(t, err)
	a, b, c := children[0], children[1], children[2]
	require.Len(t, started, 2, "只点火 wave 1")

	// 子 A 完成 → 立即合入（不等子 B）。
	pushChildBranch(t, proj, a.ID, "a.txt", "aaa")
	completeChild(t, st, a.ID)
	e.MaybeDriveParent(ctx, parent.ID)
	require.Equal(t, 1, mergedChildCount(t, st, parent.ID))
	require.Len(t, started, 2, "子 B 未完成，wave 2 不启动")
	require.Equal(t, StatusQueued, get(t, st, c.ID).Status)

	// 子 B 完成 → wave 1 全合并 → wave 2 启动。
	pushChildBranch(t, proj, b.ID, "b.txt", "bbb")
	completeChild(t, st, b.ID)
	e.MaybeDriveParent(ctx, parent.ID)
	require.Equal(t, 2, mergedChildCount(t, st, parent.ID))
	require.Len(t, started, 3)
	require.Equal(t, StatusActive, get(t, st, c.ID).Status)

	// 子 C 完成 → 全部合并 → 父收尾：摘要 + unit_test → code_review 门禁。
	pushChildBranch(t, proj, c.ID, "c.txt", "ccc")
	completeChild(t, st, c.ID)
	e.MaybeDriveParent(ctx, parent.ID)
	require.Equal(t, 3, mergedChildCount(t, st, parent.ID))

	got := get(t, st, parent.ID)
	require.Equal(t, "code_review", got.CurrentStage)
	require.Equal(t, "code_review", got.PendingGate)
	require.Equal(t, StatusActive, got.Status)
	require.Contains(t, artifactByKind(t, st, parent.ID, "summary").Content, "已合并 3 个子需求分支")
	require.Contains(t, artifactByKind(t, st, parent.ID, "summary").Content, "子A")
	// 固化在 code_review 门禁发生（真合并内容进了 fakePersister 的 diff 路径）。
	require.Contains(t, eventTypes(t, st, parent.ID), "persist_done")
	for _, et := range []string{"split", "merge_done"} {
		require.Contains(t, eventTypes(t, st, parent.ID), et)
	}
}

// TestSplitMergeConflictAndResume 冲突路径（真 git）：两个子需求改同一行 →
// 第二个合并冲突 → 父 merge_state=conflict、事件带分支名与 git 指引 →
// 模拟人工解决推送父分支 → ResumeMerge 恢复队列 → 父推进到门禁。
func TestSplitMergeConflictAndResume(t *testing.T) {
	e, st, proj := newRealEnv(t)
	ctx := context.Background()
	var started []string
	e.OnStartDelivery = func(id string) { started = append(started, id) }

	parent := &store.Delivery{ProjectID: proj.ID, Title: "父需求", Status: StatusActive, CurrentStage: "intake"}
	require.NoError(t, st.CreateDelivery(ctx, parent))
	require.NoError(t, e.Start(ctx, parent.ID))
	_, err := e.ApproveWithSplit(ctx, parent.ID, []store.ChildSpec{
		{Title: "子A", Wave: 1}, {Title: "子B", Wave: 1}, {Title: "子C", Wave: 2},
	})
	require.NoError(t, err)
	children, err := st.ListChildDeliveries(ctx, parent.ID)
	require.NoError(t, err)
	a, b, c := children[0], children[1], children[2]

	// 子 A 先合入（same.txt = from A）。
	pushChildBranch(t, proj, a.ID, "same.txt", "from A\n")
	completeChild(t, st, a.ID)
	e.MaybeDriveParent(ctx, parent.ID)

	// 子 B 改同一行 → 冲突。
	pushChildBranch(t, proj, b.ID, "same.txt", "from B\n")
	completeChild(t, st, b.ID)
	e.MaybeDriveParent(ctx, parent.ID)

	got := get(t, st, parent.ID)
	require.Equal(t, MergeStateConflict, got.MergeState)
	require.Equal(t, "code_gen", got.CurrentStage, "冲突时父停在实现阶段")
	pl := eventPayload(t, st, parent.ID, "merge_conflict")
	require.Equal(t, b.ID, pl["child_id"])
	require.ElementsMatch(t, []any{"infera/" + a.ID[:8], "infera/" + b.ID[:8]}, pl["branches"])
	require.Contains(t, pl["instructions"], "git merge origin/infera/"+b.ID[:8])
	require.Contains(t, pl["instructions"], "git push origin infera/"+parent.ID[:8])

	// wave 2 不启动；再次驱动只记排队事件。
	require.Equal(t, StatusQueued, get(t, st, c.ID).Status)
	e.MaybeDriveParent(ctx, parent.ID)
	require.Contains(t, eventTypes(t, st, parent.ID), "merge_queued")
	require.Equal(t, MergeStateConflict, get(t, st, parent.ID).MergeState)

	// 校验：非 conflict 状态下 ResumeMerge 报错。
	require.Error(t, e.ResumeMerge(ctx, children[1].ID), "子需求不是 split 父")

	// 模拟人工：克隆 origin、基于 main 建父分支、依次合并两个子分支、解冲突、推送。
	human := filepath.Join(t.TempDir(), "human")
	g := git.New()
	require.NoError(t, g.Clone(ctx, proj.RepoURL, "main", human))
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = human
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=h", "GIT_AUTHOR_EMAIL=h@h", "GIT_COMMITTER_NAME=h", "GIT_COMMITTER_EMAIL=h@h")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	runGit("fetch", "origin")
	runGit("checkout", "-b", "infera/"+parent.ID[:8], "origin/main")
	runGit("fetch", "origin", "infera/"+a.ID[:8])
	runGit("merge", "--no-edit", "FETCH_HEAD")
	// 合并 B 会冲突：写成两边都保留的解决内容。
	runGit("fetch", "origin", "infera/"+b.ID[:8])
	mergeB := exec.Command("git", "merge", "--no-edit", "FETCH_HEAD")
	mergeB.Dir = human
	_ = mergeB.Run() // 预期非零
	require.NoError(t, os.WriteFile(filepath.Join(human, "same.txt"), []byte("resolved A+B\n"), 0o644))
	runGit("add", "-A")
	runGit("commit", "-m", "resolve")
	runGit("push", "origin", "infera/"+parent.ID[:8])

	// ResumeMerge：fetch 父分支 reset → 恢复队列（B 已在解决分支里，合并即 up to date）。
	require.NoError(t, e.ResumeMerge(ctx, parent.ID))
	require.Equal(t, "", get(t, st, parent.ID).MergeState)
	require.Equal(t, 2, mergedChildCount(t, st, parent.ID))
	require.Contains(t, eventTypes(t, st, parent.ID), "merge_resumed")

	// wave 2 恢复调度后启动；完成后父推进到 code_review。
	require.Equal(t, StatusActive, get(t, st, c.ID).Status)
	pushChildBranch(t, proj, c.ID, "c.txt", "ccc")
	completeChild(t, st, c.ID)
	e.MaybeDriveParent(ctx, parent.ID)
	got = get(t, st, parent.ID)
	require.Equal(t, "code_review", got.PendingGate)
	require.Equal(t, "", got.MergeState)
}

// TestChildCompletionDrivesParentAsync advance DONE 的异步父驱动钩子：
// 子需求走完整流水线完成（无固化分支 → merge_skipped）→ 父自动收尾推进到 code_review。
func TestChildCompletionDrivesParentAsync(t *testing.T) {
	e, st, proj := newRealEnv(t)
	ctx := context.Background()

	// 手工摆好 split 局面（不走 ApproveWithSplit，聚焦 advance 钩子）。
	parent := &store.Delivery{ProjectID: proj.ID, Title: "父", Status: StatusActive, CurrentStage: "code_gen", SplitMode: true}
	require.NoError(t, st.CreateDelivery(ctx, parent))
	child := &store.Delivery{ProjectID: proj.ID, Title: "子", Status: StatusActive, CurrentStage: "intake", ParentID: parent.ID, Wave: 1}
	require.NoError(t, st.CreateDelivery(ctx, child))

	// 子需求跑完整流水线（fake agent 不改文件，persist 推不出分支）。
	require.NoError(t, e.Start(ctx, child.ID))
	require.NoError(t, e.Approve(ctx, child.ID))
	require.NoError(t, e.Continue(ctx, child.ID))
	require.Equal(t, "code_review", get(t, st, child.ID).PendingGate)
	require.NoError(t, e.Approve(ctx, child.ID))
	require.Equal(t, StatusCompleted, get(t, st, child.ID).Status)

	// 异步钩子驱动父：无分支 → merge_skipped 仍记 durable 标记 → 收尾推进。
	waitUntil(t, 15*time.Second, func() bool {
		return get(t, st, parent.ID).PendingGate == "code_review"
	}, "parent reaches code_review")
	require.Contains(t, eventTypes(t, st, parent.ID), "merge_skipped")
	require.Equal(t, 1, mergedChildCount(t, st, parent.ID))
	require.Contains(t, artifactByKind(t, st, parent.ID, "summary").Content, "已合并 1 个子需求分支")
	require.False(t, strings.Contains(get(t, st, parent.ID).MergeState, "conflict"))
}
