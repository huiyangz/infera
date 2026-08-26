package syncsvc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/tasksource"
)

// 本文件覆盖任务描述「上游优先」编辑编排（INFERA-298 冻结契约）：
//
//	Editor.UpdateDeliveryDescription(deliveryID, description)
//	  → 校验 → 读交付 → 上游 PUT 描述（suppress_run）→ GetIssue 读回 →
//	    本地只落描述一列（读回值），其余字段不动。
//
// 数据所有权：上游是描述的 source of truth。本地不写独立值——落库的描述
// 永远取自上游读回（或已确认 2xx 的请求体降级值），因此下一轮全量同步
// 导入的是同一个值，不会回滚。

// fakeDescUpstream 一体两用：既是 IssueEditor 的替身（描述更新 + 单条读回），
// 也是 Fetcher 的替身（全量同步拉取）。两个面读同一份 issues 状态——
// 「编辑后再同步不回滚」的 round-trip 因此跑在真 SyncNow 上，而不是模拟。
type fakeDescUpstream struct {
	issues   map[string]*tasksource.Issue
	projects []tasksource.Project

	putErr error
	getErr error

	puts []struct{ issueID, description string }
	gets int
}

func (f *fakeDescUpstream) UpdateIssueDescription(_ context.Context, issueID, description string) error {
	f.puts = append(f.puts, struct{ issueID, description string }{issueID, description})
	if f.putErr != nil {
		return f.putErr
	}
	f.issues[issueID].Description = ptr(description)
	return nil
}

func (f *fakeDescUpstream) GetIssue(_ context.Context, idOrKey string) (tasksource.Issue, error) {
	f.gets++
	if f.getErr != nil {
		return tasksource.Issue{}, f.getErr
	}
	iss, ok := f.issues[idOrKey]
	if !ok {
		return tasksource.Issue{}, errors.New("issue 不存在")
	}
	return *iss, nil
}

func (f *fakeDescUpstream) ListProjects(context.Context) ([]tasksource.Project, error) {
	return f.projects, nil
}

func (f *fakeDescUpstream) ListIssues(context.Context) ([]tasksource.Issue, error) {
	out := make([]tasksource.Issue, 0, len(f.issues))
	for _, iss := range f.issues {
		out = append(out, *iss)
	}
	return out, nil
}

func (f *fakeDescUpstream) ListLabels(context.Context) ([]tasksource.Label, error) {
	return nil, nil
}

func (f *fakeDescUpstream) ListProjectResources(context.Context, string) ([]tasksource.ProjectResource, error) {
	return nil, nil
}

// newDescFixture 装配一套描述编辑测试件：一个上游项目 + 一条已同步落库的
// 交付（经真 SyncNow 导入，非手工种子），返回可继续注入错误的替身。
func newDescFixture(t *testing.T) (*fakeDescUpstream, store.Store, *Editor, *Service) {
	t.Helper()
	up := &fakeDescUpstream{
		projects: []tasksource.Project{proj("ext-prj-1", "自动闭环")},
		issues: map[string]*tasksource.Issue{
			"iss-1": {
				ID: "iss-1", Identifier: "INFERA-78", Title: "任务标题",
				Description: ptr("旧描述"), Status: "in_progress", Priority: "high",
				ProjectID: ptr("ext-prj-1"), UpdatedAt: time.Now(),
			},
		},
	}
	st := store.NewMemory()
	svc := New(up, st)
	_, err := svc.SyncNow(context.Background())
	require.NoError(t, err)

	ed, err := NewEditor(up, st)
	require.NoError(t, err)
	return up, st, ed, svc
}

// mirroredDelivery 按外部 issue id 取已落库的交付（唯一项目内查找）。
func mirroredDelivery(t *testing.T, st store.Store, externalID string) store.Delivery {
	t.Helper()
	ps, err := st.ListProjects(context.Background())
	require.NoError(t, err)
	require.Len(t, ps, 1)
	ds, err := st.ListProjectDeliveries(context.Background(), ps[0].ID)
	require.NoError(t, err)
	for _, d := range ds {
		if d.ExternalIssueID == externalID {
			return d
		}
	}
	t.Fatalf("找不到外部 issue %s 对应的交付", externalID)
	return store.Delivery{}
}

// TestUpdateDeliveryDescriptionRoundTrip 是本卡的验收主证：编辑 → 上游生效
// → 本地随读回生效 → 再次全量同步后本地描述仍是编辑后的值（不被上游快照
// 整行 upsert 回滚）。
func TestUpdateDeliveryDescriptionRoundTrip(t *testing.T) {
	up, st, ed, svc := newDescFixture(t)
	ctx := context.Background()

	d := mirroredDelivery(t, st, "iss-1")
	require.Equal(t, "旧描述", d.Description, "前置：同步镜像已带旧描述")

	const edited = "## 编辑后的描述\n\n- 新增验收项"
	got, err := ed.UpdateDeliveryDescription(ctx, d.ID, edited)
	require.NoError(t, err)
	require.Equal(t, edited, got.Description)

	// 上游确实收到了这次写（且只写了一次）。
	require.Len(t, up.puts, 1)
	require.Equal(t, "iss-1", up.puts[0].issueID)
	require.Equal(t, edited, up.puts[0].description)

	// 本地已随上游读回生效，无需等下一轮同步。
	require.Equal(t, edited, mirroredDelivery(t, st, "iss-1").Description)

	// 验收：再次全量同步（importIssue 整行 upsert 描述）后仍是编辑后的值。
	_, err = svc.SyncNow(ctx)
	require.NoError(t, err)
	require.Equal(t, edited, mirroredDelivery(t, st, "iss-1").Description,
		"上游已生效的编辑不得被下一轮同步覆盖")
}

// TestUpdateDeliveryDescriptionKeepsEngineFields：描述编辑只动描述一列。
// 停在门禁的交付（active + pending_gate）改描述不得被打回 queued / 丢门禁
// ——那是整行重导入（UpsertDeliveryByExternalID）会做的事，本路径刻意避开。
func TestUpdateDeliveryDescriptionKeepsEngineFields(t *testing.T) {
	_, st, ed, _ := newDescFixture(t)
	ctx := context.Background()

	d := mirroredDelivery(t, st, "iss-1")
	d.Status = "active"
	d.CurrentStage = "spec"
	d.PendingGate = "spec_approval"
	d.FailCount = 2
	require.NoError(t, st.UpdateDelivery(ctx, &d))

	got, err := ed.UpdateDeliveryDescription(ctx, d.ID, "编辑后的描述")
	require.NoError(t, err)
	require.Equal(t, "active", got.Status)
	require.Equal(t, "spec", got.CurrentStage)
	require.Equal(t, "spec_approval", got.PendingGate)
	require.Equal(t, 2, got.FailCount)

	after := mirroredDelivery(t, st, "iss-1")
	require.Equal(t, "active", after.Status)
	require.Equal(t, "spec_approval", after.PendingGate)
	require.Equal(t, "编辑后的描述", after.Description)
}

// TestUpdateDeliveryDescriptionValidation：空/纯空白/超长描述在进上游之前
// 就被拒（ErrInvalid），不产生上游写。
func TestUpdateDeliveryDescriptionValidation(t *testing.T) {
	up, st, ed, _ := newDescFixture(t)
	d := mirroredDelivery(t, st, "iss-1")

	cases := []struct {
		name string
		desc string
	}{
		{"空描述", ""},
		{"纯空白", "   \n\t "},
		{"超长", strings.Repeat("a", MaxDescriptionBytes+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ed.UpdateDeliveryDescription(context.Background(), d.ID, tc.desc)
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
	require.Empty(t, up.puts, "校验失败不得打上游")
}

// TestUpdateDeliveryDescriptionNotFound：未知交付 id → store.ErrNotFound。
func TestUpdateDeliveryDescriptionNotFound(t *testing.T) {
	_, _, ed, _ := newDescFixture(t)
	_, err := ed.UpdateDeliveryDescription(context.Background(), "0b7e8b1a-0000-4000-8000-000000000000", "x")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestUpdateDeliveryDescriptionNotMirrored：非同步来源的交付（无上游映射）
// 没有可写的上游对象 → ErrNotMirrored，且不打上游、不动本地。
func TestUpdateDeliveryDescriptionNotMirrored(t *testing.T) {
	up, st, ed, _ := newDescFixture(t)
	ctx := context.Background()

	ps, err := st.ListProjects(ctx)
	require.NoError(t, err)
	local := &store.Delivery{ProjectID: ps[0].ID, Title: "本地建", Description: "本地描述", Status: "queued"}
	require.NoError(t, st.CreateDelivery(ctx, local))

	_, err = ed.UpdateDeliveryDescription(ctx, local.ID, "改不动")
	require.ErrorIs(t, err, ErrNotMirrored)
	require.Empty(t, up.puts)

	got, err := st.GetDelivery(ctx, local.ID)
	require.NoError(t, err)
	require.Equal(t, "本地描述", got.Description, "本地交付不被触碰")
}

// TestUpdateDeliveryDescriptionUpstreamFailure：上游写失败如实上抛，本地
// 描述保持原值（不落半截状态）。
func TestUpdateDeliveryDescriptionUpstreamFailure(t *testing.T) {
	up, st, ed, _ := newDescFixture(t)
	up.putErr = errors.New("上游打雷")

	d := mirroredDelivery(t, st, "iss-1")
	_, err := ed.UpdateDeliveryDescription(context.Background(), d.ID, "新描述")
	require.Error(t, err)
	require.Contains(t, err.Error(), "上游打雷")
	require.Equal(t, "旧描述", mirroredDelivery(t, st, "iss-1").Description,
		"上游没写成，本地不得先行变更")
}

// TestUpdateDeliveryDescriptionReadbackDegrades：上游写已 2xx、读回失败——
// 编辑本身是成功的，不转为错误（重试会重复打上游）；本地按已确认的请求体
// 落描述（降级值），下一轮同步用上游真相校正。
func TestUpdateDeliveryDescriptionReadbackDegrades(t *testing.T) {
	up, st, ed, _ := newDescFixture(t)
	up.getErr = errors.New("读回失败")

	d := mirroredDelivery(t, st, "iss-1")
	got, err := ed.UpdateDeliveryDescription(context.Background(), d.ID, "降级路径的值")
	require.NoError(t, err)
	require.Equal(t, "降级路径的值", got.Description)
	require.Equal(t, "降级路径的值", mirroredDelivery(t, st, "iss-1").Description)
}
