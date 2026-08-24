package syncsvc

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/db"
	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/tasksource"
)

// pgStore 取会话专属测试库（TEST_DATABASE_URL，与 store 包同约定：未设置跳过）。
// 共享测试库并发互 TRUNCATE 是既有坑——跑本文件必须配会话专属库 + `go test -p 1`。
func pgStore(t *testing.T) store.Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL 未设置")
	}
	if err := db.Migrate(url); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Connect(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, _ = pool.Exec(context.Background(), `TRUNCATE delivery_labels, labels, events, artifacts, stage_runs, deliveries, projects, pipeline_bindings, agents, requirements, gate_cards, audit_log, project_settings`)
	return store.NewPg(pool)
}

// findByExtID 在任意 Store 上按 上游 issue ID 找已落库的 delivery。
func findByExtID(t *testing.T, st store.Store, extID string) *store.Delivery {
	t.Helper()
	ctx := context.Background()
	projs, err := st.ListProjects(ctx)
	require.NoError(t, err)
	for _, p := range projs {
		ds, err := st.ListProjectDeliveries(ctx, p.ID)
		require.NoError(t, err)
		for _, d := range ds {
			if d.ExternalIssueID == extID {
				return &d
			}
		}
	}
	return nil
}

// TestPgSyncImportRoundtrip 同步链路打真库：拉取 → 映射 → upsert 落 SQL，
// 父子关系解析与幂等（重复触发不产生重复行、infera 侧字段保留）以真实
// Pg 语义验证（Memory 与 Pg 的 upsert 语义一致性归 T02 测试，此处验证
// 编排层与真实 SQL 面的接缝）。
func TestPgSyncImportRoundtrip(t *testing.T) {
	st := pgStore(t)
	ctx := context.Background()
	f := &fakeFetch{
		projects: []tasksource.Project{proj("m-prj-1", "自动闭环")},
		labels:   []tasksource.Label{lbl("lbl-auto", "auto", "#22c55e"), lbl("lbl-cand", "候选", "#a855f7")},
		issues: []tasksource.Issue{
			{
				ID: "m-iss-1", Identifier: "INFERA-1", Title: "父需求", Status: "in_progress",
				Priority: "urgent", Description: ptr("父描述"), ProjectID: ptr("m-prj-1"),
				Labels:    []tasksource.Label{lbl("lbl-auto", "auto", "#22c55e"), lbl("lbl-cand", "候选", "#a855f7")},
				UpdatedAt: time.Now(),
			},
			{
				ID: "m-iss-2", Identifier: "INFERA-2", Title: "子需求", Status: "done",
				ProjectID: ptr("m-prj-1"), ParentIssueID: ptr("m-iss-1"), UpdatedAt: time.Now(),
			},
		},
	}
	svc := New(f, st)
	res, err := svc.SyncNow(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, res.ProjectsImported)
	require.Equal(t, 2, res.IssuesImported)
	require.Equal(t, 2, res.LabelsImported)

	parent := findByExtID(t, st, "m-iss-1")
	require.NotNil(t, parent)
	require.Equal(t, "queued", parent.Status)
	require.NotNil(t, parent.ExternalSyncedAt)
	child := findByExtID(t, st, "m-iss-2")
	require.NotNil(t, child)
	require.Equal(t, "completed", child.Status)
	require.Equal(t, parent.ID, child.ParentID, "父子关系经外部 ID → 内部 ID 解析后落库")
	// fixture 未设置 stage：0 = 无阶段，原样落库不兜底 1（INFERA-146 语义）。
	require.Equal(t, 0, child.Wave)

	// 标签镜像打真 SQL：交付挂标 name+color 与上游一致（INFERA-219 T02）。
	got, err := st.ListDeliveryLabels(ctx, parent.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "auto", got[0].Name)
	require.Equal(t, "#22c55e", got[0].Color)
	require.Equal(t, "lbl-auto", got[0].ExternalLabelID)
	require.Equal(t, "候选", got[1].Name)
	require.Equal(t, "#a855f7", got[1].Color)

	// infera 侧推进引擎字段后重同步：行数不变、标题更新、引擎字段保留。
	first := findByExtID(t, st, "m-iss-1")
	first.CurrentStage = "code_gen"
	require.NoError(t, st.UpdateDelivery(ctx, first))
	f.issues[0].Title = "父需求（改名）"
	res2, err := svc.SyncNow(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, res2.IssuesImported)

	renamed := findByExtID(t, st, "m-iss-1")
	require.Equal(t, first.ID, renamed.ID, "同外部 ID 命中同一行")
	require.Equal(t, "父需求（改名）", renamed.Title)
	require.Equal(t, "code_gen", renamed.CurrentStage, "引擎字段不被同步覆盖")

	projs, err := st.ListProjects(ctx)
	require.NoError(t, err)
	require.Len(t, projs, 1, "重复同步不产生重复项目行")
	ds, err := st.ListProjectDeliveries(ctx, projs[0].ID)
	require.NoError(t, err)
	require.Len(t, ds, 2, "重复同步不产生重复需求行")

	// 标签库幂等（真 SQL 唯一索引）：第二轮后行数不翻倍、名称颜色一致、
	// 交付挂标不重复（关联表复合主键）。
	labels, err := st.ListLabels(ctx)
	require.NoError(t, err)
	require.Len(t, labels, 2, "重复同步不产生重复标签行")
	require.Equal(t, 2, res2.LabelsImported)
	got, err = st.ListDeliveryLabels(ctx, first.ID)
	require.NoError(t, err)
	require.Len(t, got, 2, "重复同步不产生重复关联行")

	// 上游摘标（auto）后第三轮：infera 侧同步摘除；标签库行保留。
	f.issues[0].Labels = []tasksource.Label{lbl("lbl-cand", "候选", "#a855f7")}
	res3, err := svc.SyncNow(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, res3.LabelsImported, "标签库镜像 workspace 库，摘标不删标签")
	got, err = st.ListDeliveryLabels(ctx, first.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "候选", got[0].Name, "上游已摘的 auto 同步摘除")
}
