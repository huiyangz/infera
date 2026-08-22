package syncsvc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/tasksource"
)

// 本文件覆盖需求创建编排（L202608230412-1-T01 冻结契约）：infera 项目 →
// 上游项目映射、缺省智能体=Tech Lead、autoMerge→auto label、状态两档
// （backlog|todo，缺省 backlog）、优先级透传、建卡后触发既有同步回流、
// 按 ExternalIssueID 读回同步后的行作为响应。

// fakeCreatorClient 是 IssueCreator 的测试替身：记录调用并回放结果。
type fakeCreatorClient struct {
	createIssueIn   tasksource.CreateIssueInput
	createIssueErr  error
	created         tasksource.Issue
	createIssueDone bool

	labels    []tasksource.Label
	labelsErr error

	addLabelCalls []struct{ issueID, labelID string }
	addLabelErr   error
}

func (f *fakeCreatorClient) CreateIssue(_ context.Context, in tasksource.CreateIssueInput) (tasksource.Issue, error) {
	f.createIssueIn = in
	f.createIssueDone = true
	if f.createIssueErr != nil {
		return tasksource.Issue{}, f.createIssueErr
	}
	return f.created, nil
}

func (f *fakeCreatorClient) ListLabels(context.Context) ([]tasksource.Label, error) {
	return f.labels, f.labelsErr
}

func (f *fakeCreatorClient) AddIssueLabel(_ context.Context, issueID, labelID string) error {
	f.addLabelCalls = append(f.addLabelCalls, struct{ issueID, labelID string }{issueID, labelID})
	return f.addLabelErr
}

// fakeReflowTrigger 是 SyncTrigger 的测试替身：模拟同步回流——把替身 client
// 刚建的 issue 经 UpsertDeliveryByExternalID 落进 store（真同步的落库效果），
// 或按注入错误短路（ErrSyncRunning / 失败共用）。
type fakeReflowTrigger struct {
	calls int
	err   error

	source *fakeCreatorClient // 建卡结果的持有方（回流要拿 issue 锚点落库）
	st     store.Store
	pid    string // infera 项目 id
}

func (f *fakeReflowTrigger) SyncNow(ctx context.Context) (Result, error) {
	f.calls++
	if f.err != nil {
		return Result{}, f.err
	}
	now := time.Now().UTC()
	issue := f.source.created
	err := f.st.UpsertDeliveryByExternalID(ctx, &store.Delivery{
		ProjectID: f.pid, Title: issue.Title, Status: "queued",
		ExternalIssueID: issue.ID, ExternalIssueKey: issue.Identifier,
		Assignee: "agent:lead-1", ExternalSyncedAt: &now,
	})
	return Result{}, err
}

// newCreatorTest 装配一套 Creator 测试件：种子项目（带上游映射）+ 替身。
// 返回 infera 侧项目 id（UpsertProjectByExternalID 回填）。
func newCreatorTest(t *testing.T) (*Creator, *fakeCreatorClient, *fakeReflowTrigger, store.Store, string) {
	t.Helper()
	st := store.NewMemory()
	p := &store.Project{Name: "自动闭环", ExternalProjectID: "ext-prj-1"}
	require.NoError(t, st.UpsertProjectByExternalID(context.Background(), p))
	cr := &fakeCreatorClient{created: tasksource.Issue{ID: "i-new-1", Identifier: "INFERA-178", Title: "新需求"}}
	tr := &fakeReflowTrigger{source: cr, st: st, pid: p.ID}
	c, err := NewCreator(cr, tr, st, CreatorOptions{TechLeadAgentID: "lead-1"})
	require.NoError(t, err)
	return c, cr, tr, st, p.ID
}

// TestCreatorHappyPath：缺省全解析——agent 缺省 Tech Lead、状态缺省 backlog、
// 项目走 infera 项目的上游映射、优先级透传；建卡后触发同步回流，响应返回
// 同步落库后的行（读回，非手工拼装）。
func TestCreatorHappyPath(t *testing.T) {
	c, cr, tr, _, projID := newCreatorTest(t)

	got, err := c.CreateProjectRequirement(context.Background(), projID, CreateRequirementInput{
		Title: "新需求", Description: "描述正文", Priority: "high",
	})
	require.NoError(t, err)
	require.Equal(t, "lead-1", cr.createIssueIn.AssigneeID, "缺省智能体必须是装配的 Tech Lead")
	require.Equal(t, "agent", cr.createIssueIn.AssigneeType)
	require.Equal(t, "backlog", cr.createIssueIn.Status, "状态缺省 backlog（不触发 agent run）")
	require.Equal(t, "high", cr.createIssueIn.Priority, "优先级透传")
	require.Equal(t, "ext-prj-1", cr.createIssueIn.ProjectID, "项目缺省走 infera 项目的上游映射")
	require.Equal(t, "新需求", cr.createIssueIn.Title)
	require.Equal(t, "描述正文", cr.createIssueIn.Description)
	require.Equal(t, 1, tr.calls, "建卡成功后必须触发一轮同步回流")
	require.Empty(t, cr.addLabelCalls, "autoMerge 缺省 false 不打标")

	// 响应是同步落库后的行：infera 侧 id 非空、外部锚点齐全。
	require.NotEmpty(t, got.ID, "响应必须是同步读回的行（infera 侧 id）")
	require.Equal(t, "i-new-1", got.ExternalIssueID)
	require.Equal(t, "INFERA-178", got.ExternalIssueKey)
	require.Equal(t, projID, got.ProjectID)
}

// TestCreatorAutoMerge：autoMerge=true → 建卡后按名字解析 auto label 并打标。
func TestCreatorAutoMerge(t *testing.T) {
	c, cr, _, _, projID := newCreatorTest(t)
	cr.labels = []tasksource.Label{
		{ID: "lbl-bug", Name: "bug"},
		{ID: "lbl-auto", Name: "auto"},
	}

	_, err := c.CreateProjectRequirement(context.Background(), projID, CreateRequirementInput{
		Title: "自动合并需求", AutoMerge: true, Status: "todo",
	})
	require.NoError(t, err)
	require.Len(t, cr.addLabelCalls, 1)
	require.Equal(t, "i-new-1", cr.addLabelCalls[0].issueID)
	require.Equal(t, "lbl-auto", cr.addLabelCalls[0].labelID, "必须按名字解析 auto label 的 id")
}

// TestCreatorAutoMergeMissingLabelFailFast：workspace 无 auto label → 建卡前
// 报错（fail-fast，不留无标签的半成品需求）。
func TestCreatorAutoMergeMissingLabelFailFast(t *testing.T) {
	c, cr, _, _, projID := newCreatorTest(t)
	cr.labels = []tasksource.Label{{ID: "lbl-bug", Name: "bug"}}

	_, err := c.CreateProjectRequirement(context.Background(), projID, CreateRequirementInput{
		Title: "自动合并需求", AutoMerge: true,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "auto")
	require.False(t, cr.createIssueDone, "label 解析失败不得建卡")
}

// TestCreatorExplicitAgentAndStatusTodo：显式智能体与 todo 状态透传。
func TestCreatorExplicitAgentAndStatusTodo(t *testing.T) {
	c, cr, _, _, projID := newCreatorTest(t)

	_, err := c.CreateProjectRequirement(context.Background(), projID, CreateRequirementInput{
		Title: "指定智能体", Status: "todo", AgentID: "agent-9",
	})
	require.NoError(t, err)
	require.Equal(t, "agent-9", cr.createIssueIn.AssigneeID, "显式智能体优先于 Tech Lead 缺省")
	require.Equal(t, "todo", cr.createIssueIn.Status, "状态两档透传")
}

// TestCreatorValidation：非法输入在触达上游前拦截。
func TestCreatorValidation(t *testing.T) {
	c, cr, _, _, projID := newCreatorTest(t)

	cases := []struct {
		name string
		in   CreateRequirementInput
	}{
		{"空标题", CreateRequirementInput{Title: "   "}},
		{"状态超出两档", CreateRequirementInput{Title: "t", Status: "done"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.CreateProjectRequirement(context.Background(), projID, tc.in)
			require.ErrorIs(t, err, ErrInvalid)
			require.False(t, cr.createIssueDone, "非法输入不得触达上游")
		})
	}
}

// TestCreatorProjectNotMapped：infera 项目无上游映射（从未同步/本地建项）→
// 明确错误，不触达上游。
func TestCreatorProjectNotMapped(t *testing.T) {
	st := store.NewMemory()
	p := &store.Project{Name: "纯本地项目"}
	require.NoError(t, st.CreateProject(context.Background(), p))
	cr := &fakeCreatorClient{}
	c, err := NewCreator(cr, &fakeReflowTrigger{}, st, CreatorOptions{TechLeadAgentID: "lead-1"})
	require.NoError(t, err)

	_, err = c.CreateProjectRequirement(context.Background(), p.ID, CreateRequirementInput{Title: "t"})
	require.ErrorIs(t, err, ErrProjectNotMapped)
	require.False(t, cr.createIssueDone)
}

// TestCreatorSyncRunningStillSucceeds：同步进行中（ErrSyncRunning）不阻断创建
// ——请求已成功，回流交给在途/后续轮次；响应退化为按上游回包拼装的行
// （无 infera 侧 id，外部锚点齐全）。
func TestCreatorSyncRunningStillSucceeds(t *testing.T) {
	st := store.NewMemory()
	p := &store.Project{Name: "自动闭环", ExternalProjectID: "ext-prj-1"}
	require.NoError(t, st.UpsertProjectByExternalID(context.Background(), p))
	cr := &fakeCreatorClient{created: tasksource.Issue{ID: "i-new-1", Identifier: "INFERA-178", Title: "新需求", Priority: "high"}}
	tr := &fakeReflowTrigger{source: cr, st: st, pid: p.ID, err: ErrSyncRunning}
	c, err := NewCreator(cr, tr, st, CreatorOptions{TechLeadAgentID: "lead-1"})
	require.NoError(t, err)

	got, err := c.CreateProjectRequirement(context.Background(), p.ID, CreateRequirementInput{Title: "新需求", Priority: "high"})
	require.NoError(t, err, "创建已成功，同步占用不是创建失败")
	require.Empty(t, got.ID, "未回流成功时无 infera 侧 id")
	require.Equal(t, "i-new-1", got.ExternalIssueID)
	require.Equal(t, "INFERA-178", got.ExternalIssueKey)
	require.Equal(t, "新需求", got.Title)
	require.Equal(t, "high", got.Priority)
	require.Equal(t, "agent:lead-1", got.Assignee)
	require.Equal(t, "queued", got.Status, "未回流时按同步侧词表预翻（非终态→queued）")
}

// TestCreatorUpstreamFailure：上游建卡失败 → 错误上抛，不触发同步。
func TestCreatorUpstreamFailure(t *testing.T) {
	c, cr, tr, _, projID := newCreatorTest(t)
	cr.createIssueErr = errors.New("HTTP 500")

	_, err := c.CreateProjectRequirement(context.Background(), projID, CreateRequirementInput{Title: "t"})
	require.ErrorContains(t, err, "HTTP 500")
	require.Zero(t, tr.calls, "建卡失败不得触发同步")
}

// TestCreatorConstructorRequiresTechLead：Tech Lead 缺失在构造期报错
// （reqservice.Options 同款必填校验——缺项漏到运行期只会变成难排查的派发失败）。
func TestCreatorConstructorRequiresTechLead(t *testing.T) {
	_, err := NewCreator(&fakeCreatorClient{}, &fakeReflowTrigger{}, store.NewMemory(), CreatorOptions{})
	require.ErrorContains(t, err, "TechLeadAgentID")
}
