package syncsvc

// 标签镜像（INFERA-219 T02）：同步轮内（1）按上游标签 id 幂等 upsert 标签库
// （名称+颜色一致）；（2）给镜像交付挂标，键 = external_issue_id；（3）全量镜像
// 语义——上游摘标后重复同步，infera 侧同步摘除（只加不减会让标签越积越多）。
// 复用的测试基建（fakeFetch/iss/proj/findByExternalIssueID）在 syncsvc_test.go。

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/tasksource"
)

// lbl 造一个 workspace 标签库条目。
func lbl(id, name, color string) tasksource.Label {
	return tasksource.Label{ID: id, Name: name, Color: color}
}

// issWithLabels 造带标签的 issue（labels 面的 iss 变体）。
func issWithLabels(id, key, title, projectID string, labels ...tasksource.Label) tasksource.Issue {
	i := iss(id, key, title, projectID)
	i.Labels = labels
	return i
}

// labelsOfDelivery 读镜像交付当前挂的标签（按 name 升序，store 语义）。
func labelsOfDelivery(t *testing.T, st *store.Memory, extIssueID string) []store.Label {
	t.Helper()
	d := findByExternalIssueID(t, st, extIssueID)
	require.NotNil(t, d, "交付 %s 应已镜像", extIssueID)
	ls, err := st.ListDeliveryLabels(context.Background(), d.ID)
	require.NoError(t, err)
	return ls
}

// --- AC: 同一轮内标签库 upsert 幂等——连续两轮行数不变、名称颜色一致 ---

func TestSyncLabelsLibraryIdempotent(t *testing.T) {
	st := store.NewMemory()
	f := &fakeFetch{
		projects: []tasksource.Project{proj("m-prj-1", "自动闭环")},
		labels:   []tasksource.Label{lbl("lbl-auto", "auto", "#22c55e"), lbl("lbl-intel", "情报", "#3b82f6")},
		issues:   []tasksource.Issue{iss("m-iss-1", "INFERA-1", "需求", "m-prj-1")},
	}
	svc := New(f, st)

	res1 := syncOnce(t, svc)
	require.Equal(t, 2, res1.LabelsImported)

	// 第二轮：同名同色重复导入——行数不变、名称颜色一致，计数稳定。
	res2 := syncOnce(t, svc)
	labels, err := st.ListLabels(context.Background())
	require.NoError(t, err)
	require.Len(t, labels, 2, "重复同步不产生重复标签行")
	require.Equal(t, 2, res2.LabelsImported)

	byName := map[string]store.Label{}
	for _, l := range labels {
		byName[l.Name] = l
	}
	require.Equal(t, "#22c55e", byName["auto"].Color)
	require.Equal(t, "#3b82f6", byName["情报"].Color)
	require.Equal(t, "lbl-auto", byName["auto"].ExternalLabelID, "上游标签 id 落为幂等键")
}

// --- AC: 交付挂标与上游一致（名称+颜色） ---

func TestSyncAttachesLabelsToMirroredDeliveries(t *testing.T) {
	st := store.NewMemory()
	f := &fakeFetch{
		projects: []tasksource.Project{proj("m-prj-1", "自动闭环")},
		labels:   []tasksource.Label{lbl("lbl-auto", "auto", "#22c55e"), lbl("lbl-cand", "候选", "#a855f7")},
		issues: []tasksource.Issue{
			issWithLabels("m-iss-1", "INFERA-1", "双标签", "m-prj-1",
				lbl("lbl-auto", "auto", "#22c55e"), lbl("lbl-cand", "候选", "#a855f7")),
			issWithLabels("m-iss-2", "INFERA-2", "单标签", "m-prj-1",
				lbl("lbl-auto", "auto", "#22c55e")),
			iss("m-iss-3", "INFERA-3", "无标签", "m-prj-1"),
		},
	}
	syncOnce(t, New(f, st))

	two := labelsOfDelivery(t, st, "m-iss-1")
	require.Len(t, two, 2)
	require.Equal(t, "auto", two[0].Name)
	require.Equal(t, "#22c55e", two[0].Color)
	require.Equal(t, "候选", two[1].Name)
	require.Equal(t, "#a855f7", two[1].Color)

	one := labelsOfDelivery(t, st, "m-iss-2")
	require.Len(t, one, 1)
	require.Equal(t, "auto", one[0].Name)

	require.Empty(t, labelsOfDelivery(t, st, "m-iss-3"), "上游无标签 → 镜像无标签")
}

// --- AC: 上游摘标后再次同步，infera 侧同步摘除（不越积越多） ---

func TestSyncDetachesUpstreamRemovedLabels(t *testing.T) {
	st := store.NewMemory()
	f := &fakeFetch{
		projects: []tasksource.Project{proj("m-prj-1", "自动闭环")},
		labels:   []tasksource.Label{lbl("lbl-auto", "auto", "#22c55e"), lbl("lbl-cand", "候选", "#a855f7")},
		issues: []tasksource.Issue{
			issWithLabels("m-iss-1", "INFERA-1", "双标签", "m-prj-1",
				lbl("lbl-auto", "auto", "#22c55e"), lbl("lbl-cand", "候选", "#a855f7")),
		},
	}
	svc := New(f, st)
	syncOnce(t, svc)
	require.Len(t, labelsOfDelivery(t, st, "m-iss-1"), 2)

	// infera 侧手工挂一个本地标签（无外部 id）：不属于镜像域，同步不得动它。
	local := &store.Label{Name: "本地标记", Color: "#111111"}
	require.NoError(t, st.CreateLabel(context.Background(), local))
	d := findByExternalIssueID(t, st, "m-iss-1")
	require.NoError(t, st.AttachLabel(context.Background(), d.ID, local.ID))

	// 上游摘掉 auto，保留候选；标签库本身不变（摘标 ≠ 删标签）。
	f.issues = []tasksource.Issue{
		issWithLabels("m-iss-1", "INFERA-1", "双标签", "m-prj-1",
			lbl("lbl-cand", "候选", "#a855f7")),
	}
	syncOnce(t, svc)

	got := labelsOfDelivery(t, st, "m-iss-1")
	require.Len(t, got, 2, "候选（镜像）+ 本地标记（镜像域外，保留）")
	names := []string{got[0].Name, got[1].Name}
	require.Contains(t, names, "候选")
	require.Contains(t, names, "本地标记")
	require.NotContains(t, names, "auto", "上游已摘的标签必须同步摘除")

	// 标签库行保留：摘的是关联，不是标签本身（标签库镜像 workspace 库）。
	labels, err := st.ListLabels(context.Background())
	require.NoError(t, err)
	require.Len(t, labels, 3, "auto/候选（库）+ 本地标记")

	// 摘到一张不剩：全部镜像标签摘除，本地标记仍在。
	f.issues = []tasksource.Issue{iss("m-iss-1", "INFERA-1", "双标签", "m-prj-1")}
	syncOnce(t, svc)
	final := labelsOfDelivery(t, st, "m-iss-1")
	require.Len(t, final, 1)
	require.Equal(t, "本地标记", final[0].Name)
}

// --- 契约：被 skips 规则跳过未镜像的单不挂标签 ---

func TestSyncSkippedIssuesGetNoLabels(t *testing.T) {
	st := store.NewMemory()
	f := &fakeFetch{
		projects: []tasksource.Project{proj("m-prj-1", "自动闭环")},
		labels:   []tasksource.Label{lbl("lbl-auto", "auto", "#22c55e")},
		issues: []tasksource.Issue{
			issWithLabels("m-smoke", "INFERA-90", "[infera-e2e] 冒烟单", "m-prj-1",
				lbl("lbl-auto", "auto", "#22c55e")),
			{ID: "m-noproj", Identifier: "INFERA-91", Title: "无项目 issue", Status: "todo",
				Labels: []tasksource.Label{lbl("lbl-auto", "auto", "#22c55e")}, UpdatedAt: time.Now()},
		},
	}
	res := syncOnce(t, New(f, st))
	require.Equal(t, 0, res.IssuesImported)
	require.Equal(t, 2, res.IssuesSkipped)
	require.Nil(t, findByExternalIssueID(t, st, "m-smoke"), "冒烟单不落库")
	require.Nil(t, findByExternalIssueID(t, st, "m-noproj"), "无项目单不落库")

	// 标签库仍按 workspace 库镜像（未挂任何 issue 的标签也进库）。
	labels, err := st.ListLabels(context.Background())
	require.NoError(t, err)
	require.Len(t, labels, 1)
	require.Equal(t, "auto", labels[0].Name)
}

// --- 契约：上游字段缺失/形状不符不 fatal——issue 引用标签库未见过的标签时
// 从 issue 内嵌对象兜底落库（同 id 幂等命中同一行） ---

func TestSyncIssueLabelMissingFromLibraryUpstream(t *testing.T) {
	st := store.NewMemory()
	f := &fakeFetch{
		projects: []tasksource.Project{proj("m-prj-1", "自动闭环")},
		labels:   []tasksource.Label{lbl("lbl-auto", "auto", "#22c55e")}, // 库里没有 lbl-cand
		issues: []tasksource.Issue{
			issWithLabels("m-iss-1", "INFERA-1", "引用库外标签", "m-prj-1",
				lbl("lbl-auto", "auto", "#22c55e"), lbl("lbl-cand", "候选", "#a855f7")),
		},
	}
	res := syncOnce(t, New(f, st))
	require.Equal(t, 2, res.LabelsImported, "库外引用按内嵌对象兜底 upsert，计数含它")

	got := labelsOfDelivery(t, st, "m-iss-1")
	require.Len(t, got, 2, "兜底落库的标签同样挂上交付")

	labels, err := st.ListLabels(context.Background())
	require.NoError(t, err)
	require.Len(t, labels, 2)
}

// --- 拉取失败：标签库端点失败 → 整轮失败（与其余拉取面一致，不吞成"无标签"） ---

func TestSyncLabelsFetchFails(t *testing.T) {
	st := store.NewMemory()
	f := &fakeFetch{
		projects: []tasksource.Project{proj("m-prj-1", "自动闭环")},
		lblErr:   errors.New("tasksource: HTTP 500"),
	}
	res, err := New(f, st).SyncNow(context.Background())
	require.Error(t, err)
	require.NotEmpty(t, res.Error)

	labels, err2 := st.ListLabels(context.Background())
	require.NoError(t, err2)
	require.Empty(t, labels, "失败轮不落任何标签行")
}
