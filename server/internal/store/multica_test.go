package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// checkMulticaSync 验证 multica 同步存储面（INFERA-79 T02 冻结的契约，内存/pg 共用断言）：
// 外部来源字段 roundtrip + 按外部 ID 的幂等 upsert（重复执行不产生重复行、字段更新生效）。
func checkMulticaSync(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	// --- 项目 upsert：首次 = 插入，回读带映射字段。 ---
	p1 := &Project{Name: "自动闭环", RepoURL: "", DefaultBranch: "main", MulticaProjectID: "01m-prj-1"}
	require.NoError(t, s.UpsertProjectByMulticaID(ctx, p1))
	require.NotEmpty(t, p1.ID, "插入后回填内部 ID")
	require.NotNil(t, p1.MulticaSyncedAt, "插入后回填同步时间")
	projects, err := s.ListProjects(ctx)
	require.NoError(t, err)
	require.Len(t, projects, 1)
	require.Equal(t, "01m-prj-1", projects[0].MulticaProjectID)
	require.NotNil(t, projects[0].MulticaSyncedAt)

	// --- 项目 upsert 幂等：同外部 ID 换数据 → 行数不变、字段更新、ID 稳定。 ---
	p2 := &Project{Name: "自动闭环（改名）", MulticaProjectID: "01m-prj-1"}
	require.NoError(t, s.UpsertProjectByMulticaID(ctx, p2))
	require.Equal(t, p1.ID, p2.ID, "同外部 ID 命中同一行")
	projects, err = s.ListProjects(ctx)
	require.NoError(t, err)
	require.Len(t, projects, 1, "重复 upsert 不产生重复行")
	require.Equal(t, "自动闭环（改名）", projects[0].Name)

	// --- 项目 repo_url 覆写契约（INFERA-175）：multica 侧解析出绑定（非空）→
	// 覆写现值；解析不出（空）→ 保留现值不清空。default_branch/pinned 仍归
	// infera 侧配置，冲突分支不覆盖。 ---
	p3 := &Project{Name: "自动闭环（改名）", RepoURL: "git@github.com:huiyangz/infera.git", MulticaProjectID: "01m-prj-1"}
	require.NoError(t, s.UpsertProjectByMulticaID(ctx, p3))
	require.Equal(t, p1.ID, p3.ID, "repo_url 覆写轮仍命中同一行")
	gotProj, err := s.GetProject(ctx, p1.ID)
	require.NoError(t, err)
	require.Equal(t, "git@github.com:huiyangz/infera.git", gotProj.RepoURL, "非空 repo_url 覆写现值")

	p4 := &Project{Name: "自动闭环（改名）", MulticaProjectID: "01m-prj-1"} // RepoURL 空 = 无绑定
	require.NoError(t, s.UpsertProjectByMulticaID(ctx, p4))
	gotProj, err = s.GetProject(ctx, p1.ID)
	require.NoError(t, err)
	require.Equal(t, "git@github.com:huiyangz/infera.git", gotProj.RepoURL, "空 repo_url 不清空现值")

	// --- 非 multica 项目共存：空外部 ID 不参与唯一约束。 ---
	local := &Project{Name: "本地项目"}
	require.NoError(t, s.CreateProject(ctx, local))
	projects, err = s.ListProjects(ctx)
	require.NoError(t, err)
	require.Len(t, projects, 2)
	require.Empty(t, projects[1].MulticaProjectID)
	require.Nil(t, projects[1].MulticaSyncedAt, "非同步项目 synced_at 为 nil")

	// 外部 ID 为空 → ErrInvalid（同步入口必须带外部 ID）。
	require.ErrorIs(t, s.UpsertProjectByMulticaID(ctx, &Project{Name: "x"}), ErrInvalid)

	// --- 需求 upsert：首次 = 插入，映射字段 roundtrip。 ---
	d1 := &Delivery{
		ProjectID:       p1.ID,
		Title:           "同步需求A",
		Description:     "来自 multica 的描述",
		Status:          "active",
		MulticaIssueID:  "01m-iss-1",
		MulticaIssueKey: "INFERA-100",
		Assignee:        "小鱼儿",
		Priority:        "high",
	}
	require.NoError(t, s.UpsertDeliveryByMulticaID(ctx, d1))
	require.NotEmpty(t, d1.ID)
	require.NotNil(t, d1.MulticaSyncedAt)
	got, err := s.GetDelivery(ctx, d1.ID)
	require.NoError(t, err)
	require.Equal(t, "01m-iss-1", got.MulticaIssueID)
	require.Equal(t, "INFERA-100", got.MulticaIssueKey)
	require.Equal(t, "小鱼儿", got.Assignee)
	require.Equal(t, "high", got.Priority)
	require.NotNil(t, got.MulticaSyncedAt)
	require.Empty(t, got.ParentID, "普通需求 parent 为空")

	// 引用不存在的项目 → ErrNotFound（FK 语义，同 UpsertBinding）。
	require.ErrorIs(t, s.UpsertDeliveryByMulticaID(ctx, &Delivery{
		ProjectID: "00000000-0000-0000-0000-000000000000",
		Title:     "孤儿", MulticaIssueID: "01m-iss-x",
	}), ErrNotFound)

	// 外部 ID 为空 → ErrInvalid。
	require.ErrorIs(t, s.UpsertDeliveryByMulticaID(ctx, &Delivery{ProjectID: p1.ID, Title: "x"}), ErrInvalid)

	// --- 引擎侧字段推进后，同步 upsert 不得覆盖引擎/门禁字段。 ---
	got.CurrentStage = "code_gen"
	got.PendingGate = "code_review"
	got.FailCount = 2
	require.NoError(t, s.UpdateDelivery(ctx, got))

	d1b := &Delivery{
		ProjectID:       p1.ID,
		Title:           "同步需求A（改名）",
		Description:     "描述更新",
		Status:          "active",
		MulticaIssueID:  "01m-iss-1",
		MulticaIssueKey: "INFERA-100",
		Assignee:        "别人",
		Priority:        "low",
	}
	require.NoError(t, s.UpsertDeliveryByMulticaID(ctx, d1b))
	require.Equal(t, d1.ID, d1b.ID, "同外部 issue ID 命中同一行")

	deliveries, err := s.ListProjectDeliveries(ctx, p1.ID)
	require.NoError(t, err)
	require.Len(t, deliveries, 1, "重复 upsert 不产生重复行")
	got, err = s.GetDelivery(ctx, d1.ID)
	require.NoError(t, err)
	// 外部来源字段更新生效
	require.Equal(t, "同步需求A（改名）", got.Title)
	require.Equal(t, "描述更新", got.Description)
	require.Equal(t, "别人", got.Assignee)
	require.Equal(t, "low", got.Priority)
	// 引擎侧字段保持
	require.Equal(t, "code_gen", got.CurrentStage)
	require.Equal(t, "code_review", got.PendingGate)
	require.Equal(t, 2, got.FailCount)

	// --- 子需求：父子关系随同步落库，ListChildDeliveries 可见。 ---
	child := &Delivery{
		ProjectID:      p1.ID,
		Title:          "同步子需求",
		Status:         "queued",
		ParentID:       d1.ID,
		Wave:           1,
		MulticaIssueID: "01m-iss-2",
	}
	require.NoError(t, s.UpsertDeliveryByMulticaID(ctx, child))
	children, err := s.ListChildDeliveries(ctx, d1.ID)
	require.NoError(t, err)
	require.Len(t, children, 1)
	require.Equal(t, "01m-iss-2", children[0].MulticaIssueID)
	require.Equal(t, d1.ID, children[0].ParentID)
	require.Equal(t, 1, children[0].Wave)

	// 子需求重复同步：换 wave/parent 仍命中同一行。
	child2 := &Delivery{
		ProjectID:      p1.ID,
		Title:          "同步子需求",
		Status:         "queued",
		ParentID:       d1.ID,
		Wave:           2,
		MulticaIssueID: "01m-iss-2",
	}
	require.NoError(t, s.UpsertDeliveryByMulticaID(ctx, child2))
	require.Equal(t, child.ID, child2.ID)
	children, err = s.ListChildDeliveries(ctx, d1.ID)
	require.NoError(t, err)
	require.Len(t, children, 1)
	require.Equal(t, 2, children[0].Wave, "wave 随同步更新")

	// --- UpdateDelivery 全行覆盖不得冲掉同步映射字段。 ---
	got, err = s.GetDelivery(ctx, d1.ID)
	require.NoError(t, err)
	got.Title = "引擎改名"
	require.NoError(t, s.UpdateDelivery(ctx, got))
	got, err = s.GetDelivery(ctx, d1.ID)
	require.NoError(t, err)
	require.Equal(t, "01m-iss-1", got.MulticaIssueID, "UpdateDelivery 不冲掉同步映射")
	require.Equal(t, "别人", got.Assignee)
	require.Equal(t, "01m-prj-1", func() string {
		pp, err := s.GetProject(ctx, p1.ID)
		require.NoError(t, err)
		return pp.MulticaProjectID
	}())

	// --- 全表行数收口：1 本地项目 + 1 同步项目；2 条同步需求。 ---
	projects, err = s.ListProjects(ctx)
	require.NoError(t, err)
	require.Len(t, projects, 2)
	deliveries, err = s.ListProjectDeliveries(ctx, p1.ID)
	require.NoError(t, err)
	require.Len(t, deliveries, 2)
}

func TestMemoryMulticaSync(t *testing.T) {
	checkMulticaSync(t, NewMemory())
}

func TestPgMulticaSync(t *testing.T) {
	checkMulticaSync(t, testPool(t))
}
