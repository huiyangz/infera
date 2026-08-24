package tasksource

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func strPtr(s string) *string { return &s }

// TestMapProject：项目级字段映射——标题/描述/状态/负责人/优先级全部落到
// 快照面；描述与负责人的可空语义在映射层归一（nil → ""/零值 ActorRef），
// 快照消费方（T02/T03）不再面对指针。
func TestMapProject(t *testing.T) {
	desc := "infera 需求闭环"
	leadType := "member"
	leadID := "m-1"
	updatedAt := time.Date(2026, 8, 22, 5, 0, 0, 0, time.UTC)
	got := MapProject(Project{
		ID:          "p-1",
		Title:       "自动闭环",
		Description: &desc,
		Status:      "in_progress",
		Priority:    "high",
		LeadType:    &leadType,
		LeadID:      &leadID,
		UpdatedAt:   updatedAt,
	})

	require.Equal(t, "p-1", got.ExternalID, "外部实体以上游 id 唯一标识（幂等 upsert 的锚点）")
	require.Equal(t, "自动闭环", got.Title)
	require.Equal(t, "infera 需求闭环", got.Description)
	require.Equal(t, "in_progress", got.Status)
	require.Equal(t, "high", got.Priority)
	require.Equal(t, ActorRef{Type: "member", ID: "m-1"}, got.Lead, "负责人（lead）映射为多态 ActorRef")
	require.Equal(t, updatedAt, got.UpdatedAt)
}

// TestMapProjectEmptyOptionals：可空字段未填（nil）→ 描述归一为空串、
// 负责人归一为零值 ActorRef（Type/ID 均空 = 无负责人）。
func TestMapProjectEmptyOptionals(t *testing.T) {
	got := MapProject(Project{ID: "p-2", Title: "空项目", Status: "planned", Priority: "none"})
	require.Empty(t, got.Description)
	require.Equal(t, ActorRef{}, got.Lead, "无负责人 = 零值 ActorRef，而非 nil 指针")
}

// TestMapIssue：issue 级字段映射——标题/描述/状态/负责人/优先级 + 父子关系
// （parent_issue_id）与项目归属（project_id），外加 identifier（人读键）与
// updated_at（同步新鲜度）。
func TestMapIssue(t *testing.T) {
	desc := "薄 client 补拉取"
	assigneeType := "agent"
	assigneeID := "a-1"
	parent := "i-0"
	project := "p-1"
	updatedAt := time.Date(2026, 8, 22, 6, 0, 0, 0, time.UTC)
	got := MapIssue(Issue{
		ID:            "i-1",
		Identifier:    "INFERA-78",
		Title:         "T01 拉取面",
		Description:   &desc,
		Status:        "in_progress",
		Priority:      "high",
		AssigneeType:  &assigneeType,
		AssigneeID:    &assigneeID,
		ParentIssueID: &parent,
		ProjectID:     &project,
		Stage:         2,
		UpdatedAt:     updatedAt,
	})

	require.Equal(t, "i-1", got.ExternalID)
	require.Equal(t, "INFERA-78", got.Identifier)
	require.Equal(t, "T01 拉取面", got.Title)
	require.Equal(t, "薄 client 补拉取", got.Description)
	require.Equal(t, "in_progress", got.Status)
	require.Equal(t, "high", got.Priority)
	require.Equal(t, ActorRef{Type: "agent", ID: "a-1"}, got.Assignee, "负责人（assignee）映射为多态 ActorRef")
	require.Equal(t, "i-0", got.ParentExternalID, "父子关系：父 issue 的上游 id")
	require.Equal(t, "p-1", got.ProjectExternalID, "项目归属：issue 两级关联的锚点")
	require.Equal(t, 2, got.Stage, "stage（子任务所属阶段）随快照透传，不在这层丢弃")
	require.Equal(t, updatedAt, got.UpdatedAt)
}

// TestMapIssueLabels：逐 issue 标签映射（INFERA-219 T02）——LabelRef 携带
// 上游标签 id（挂标幂等键）+ name/color（标签库 upsert 的名称颜色一致来源）。
// 半截条目（空 id）不是合法状态，映射期丢弃，不透传带病引用；无标签归一为
// 空切片，消费方免 nil 防御。
func TestMapIssueLabels(t *testing.T) {
	got := MapIssue(Issue{
		ID: "i-1", Identifier: "INFERA-1", Title: "带标签", Status: "todo",
		Labels: []Label{
			{ID: "lbl-auto", Name: "auto", Color: "#22c55e"},
			{ID: "lbl-intel", Name: "情报", Color: "#3b82f6"},
			{ID: "", Name: "半截条目", Color: "#ff0000"}, // 空 id：不是合法引用
		},
	})
	require.Equal(t, []LabelRef{
		{ExternalID: "lbl-auto", Name: "auto", Color: "#22c55e"},
		{ExternalID: "lbl-intel", Name: "情报", Color: "#3b82f6"},
	}, got.Labels)

	empty := MapIssue(Issue{ID: "i-2", Identifier: "INFERA-2", Title: "无标签", Status: "todo"})
	require.Empty(t, empty.Labels)
	require.NotNil(t, empty.Labels, "无标签归一为空切片，不是 nil")

	onlyHalf := MapIssue(Issue{ID: "i-3", Title: "全是半截", Status: "todo",
		Labels: []Label{{ID: "", Name: "x"}}})
	require.Empty(t, onlyHalf.Labels, "空 id 条目映射期丢弃")
}

// TestMapIssueEmptyOptionals：顶层无父、未指派、无描述 → 全部归一为空值，
// 消费方无需 nil 防御。
func TestMapIssueEmptyOptionals(t *testing.T) {
	got := MapIssue(Issue{ID: "i-2", Identifier: "INFERA-77", Title: "父需求", Status: "todo", Priority: "none"})
	require.Empty(t, got.Description)
	require.Equal(t, ActorRef{}, got.Assignee)
	require.Empty(t, got.ParentExternalID, "顶层 issue：无父")
	require.Empty(t, got.ProjectExternalID, "未挂项目")
	require.Zero(t, got.Stage, "stage 是普通 int 字段：未填 = 0，无指针语义")
	require.Empty(t, got.Labels, "未带标签归一为空")
}

// TestMapIssueParentChildRoundTrip：父子两级映射后仍可按 ExternalID 重建
// 关系——子.ParentExternalID == 父.ExternalID（T02 落库、T03 组装的基石）。
func TestMapIssueParentChildRoundTrip(t *testing.T) {
	parent := MapIssue(Issue{ID: "i-parent", Title: "父", Status: "todo"})
	childID := "i-child"
	child := MapIssue(Issue{ID: childID, Title: "子", Status: "todo", ParentIssueID: strPtr("i-parent")})
	require.Equal(t, parent.ExternalID, child.ParentExternalID, "子快照的父引用必须能对上父快照的外部 id")
}

// TestMapPreservesSourceVocabulary：状态/优先级保留上游原词表透传——
// 快照是"外部事实的结构化"，向 infera 词表（如 delivery status）的翻译
// 语义属于消费方（T02/T03），本层不发明对照表。
func TestMapPreservesSourceVocabulary(t *testing.T) {
	for _, status := range []string{"backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"} {
		require.Equal(t, status, MapIssue(Issue{ID: "i", Status: status}).Status)
		require.Equal(t, status, MapProject(Project{ID: "p", Status: status}).Status)
	}
	for _, priority := range []string{"urgent", "high", "medium", "low", "none"} {
		require.Equal(t, priority, MapIssue(Issue{ID: "i", Priority: priority}).Priority)
		require.Equal(t, priority, MapProject(Project{ID: "p", Priority: priority}).Priority)
	}
}

// TestMapIsPure：纯函数契约——同一输入两次映射结果相等，且映射不改变入参
// （不落库、无副作用是本层硬边界）。
func TestMapIsPure(t *testing.T) {
	in := Issue{
		ID: "i-1", Identifier: "INFERA-78", Title: "t", Status: "todo", Priority: "low",
		Description: strPtr("d"), AssigneeType: strPtr("member"), AssigneeID: strPtr("m-9"),
	}
	first := MapIssue(in)
	second := MapIssue(in)
	require.Equal(t, first, second)
	require.Equal(t, in, Issue{
		ID: "i-1", Identifier: "INFERA-78", Title: "t", Status: "todo", Priority: "low",
		Description: strPtr("d"), AssigneeType: strPtr("member"), AssigneeID: strPtr("m-9"),
	}, "映射不得改动入参")
}
