package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// seedStageRunsProject 铺数据：主项目（spec 阶段绑定了 agent、code_gen 未绑）+
// 两条需求（一条同步来源带 issue key）+ 三次运行（done/running/done），
// 外加一个空项目与一个别项目的运行（不该出现在结果里）。
// 创建/启动间 sleep 保证 started_at 严格递增（pg now() 微秒级、memory 同纳秒兜底）。
func seedStageRunsProject(t *testing.T, st Store) (main, empty *Project) {
	t.Helper()
	ctx := context.Background()
	main = &Project{Name: "主项目", RepoURL: "https://github.com/x/y", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, main))
	empty = &Project{Name: "空项目"}
	require.NoError(t, st.CreateProject(ctx, empty))

	specAgent := &Agent{Name: "规格 agent", Runner: "local"}
	require.NoError(t, st.CreateAgent(ctx, specAgent))
	require.NoError(t, st.UpsertBinding(ctx, &PipelineBinding{ProjectID: main.ID, Node: "spec", AgentID: specAgent.ID}))

	synced := &Delivery{ProjectID: main.ID, Title: "需求一", Status: "active", CurrentStage: "spec",
		ExternalIssueID: "mi-stagerun-1", ExternalIssueKey: "INFERA-9"}
	require.NoError(t, st.UpsertDeliveryByExternalID(ctx, synced))
	local := &Delivery{ProjectID: main.ID, Title: "本地需求", Status: "active", CurrentStage: "code_gen"}
	require.NoError(t, st.CreateDelivery(ctx, local))

	other := &Project{Name: "别项目"}
	require.NoError(t, st.CreateProject(ctx, other))
	otherD := &Delivery{ProjectID: other.ID, Title: "别项目需求", Status: "active"}
	require.NoError(t, st.CreateDelivery(ctx, otherD))

	start := func(d *Delivery, stage string, attempt int) *StageRun {
		// Status 显式置 running（引擎调用方如此；pg 另有列默认，memory 无）。
		r := &StageRun{DeliveryID: d.ID, Stage: stage, Attempt: attempt, Status: "running"}
		require.NoError(t, st.StartStageRun(ctx, r))
		time.Sleep(5 * time.Millisecond) // 拉开 started_at，也保证 finish 距 start ≥5ms
		return r
	}
	finish := func(r *StageRun, status string) {
		require.NoError(t, st.FinishStageRun(ctx, r.ID, status))
		time.Sleep(5 * time.Millisecond)
	}

	r1 := start(synced, "spec", 1)
	finish(r1, "done")
	r2 := start(local, "code_gen", 1)
	finish(r2, "done")
	_ = start(synced, "spec", 2) // 留 running：finished_at/duration 为 nil
	_ = start(otherD, "spec", 1) // 别项目的运行：不出现
	return main, empty
}

func checkProjectStageRuns(t *testing.T, st Store) {
	t.Helper()
	ctx := context.Background()
	main, empty := seedStageRunsProject(t, st)

	got, err := st.ProjectStageRuns(ctx, main.ID)
	require.NoError(t, err)
	require.Equal(t, main.ID, got.ProjectID)

	// 明细：started_at 倒序——running 的 spec#2 最新在前，其次 code_gen，最旧 spec#1。
	require.Len(t, got.Runs, 3, "只含主项目的运行（别项目的不出现）")
	first, second, third := got.Runs[0], got.Runs[1], got.Runs[2]
	require.Equal(t, "spec", first.Stage)
	require.Equal(t, 2, first.Attempt)
	require.Equal(t, "running", first.Status)
	require.Nil(t, first.FinishedAt, "running 未收尾 → finished_at null")
	require.Nil(t, first.DurationMS, "running 未收尾 → duration_ms null")
	require.NotNil(t, first.AgentName, "spec 已绑定 → agent_name 非 null")
	require.Equal(t, "规格 agent", *first.AgentName)
	require.Equal(t, "需求一", first.Title)
	require.Equal(t, "INFERA-9", first.ExternalIssueKey)
	require.False(t, first.StartedAt.IsZero())

	require.Equal(t, "code_gen", second.Stage)
	require.Equal(t, "done", second.Status)
	require.Nil(t, second.AgentName, "code_gen 未绑定 → agent_name null")
	require.NotNil(t, second.FinishedAt)
	require.NotNil(t, second.DurationMS, "已收尾 → duration_ms 非 null")
	require.GreaterOrEqual(t, *second.DurationMS, int64(5), "seed 里 start→finish 至少隔 5ms")
	require.Equal(t, "本地需求", second.Title)
	require.Empty(t, second.ExternalIssueKey, "本地需求 issue_key 为空串")

	require.Equal(t, "spec", third.Stage)
	require.Equal(t, 1, third.Attempt)
	require.Equal(t, "done", third.Status)
	require.NotNil(t, third.DurationMS)

	// 聚合：与明细同一窗口。spec 2 次（done 1 / running 1），code_gen 1 次 done。
	byStage := map[string]StageRunStageStats{}
	for _, s := range got.ByStage {
		byStage[s.Stage] = s
	}
	require.Len(t, byStage, 2)
	require.Equal(t, StageRunStageStats{Stage: "spec", Total: 2, Done: 1, Failed: 0, Running: 1,
		AvgMS: byStage["spec"].AvgMS, P95MS: byStage["spec"].P95MS}, byStage["spec"])
	require.Greater(t, byStage["spec"].AvgMS, float64(0), "spec 有 1 条已收尾运行，均值 > 0")
	require.Greater(t, byStage["spec"].P95MS, float64(0))
	require.Equal(t, StageRunStageStats{Stage: "code_gen", Total: 1, Done: 1, Failed: 0, Running: 0,
		AvgMS: byStage["code_gen"].AvgMS, P95MS: byStage["code_gen"].P95MS}, byStage["code_gen"])
	require.Greater(t, byStage["code_gen"].AvgMS, float64(0))

	// 空项目：空数组而非 nil。
	gotEmpty, err := st.ProjectStageRuns(ctx, empty.ID)
	require.NoError(t, err)
	require.NotNil(t, gotEmpty.Runs)
	require.Empty(t, gotEmpty.Runs)
	require.NotNil(t, gotEmpty.ByStage)
	require.Empty(t, gotEmpty.ByStage)

	// 项目不存在 → ErrNotFound。
	_, err = st.ProjectStageRuns(ctx, "0b7ddc6e-0000-4000-8000-000000000000")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryProjectStageRuns(t *testing.T) {
	checkProjectStageRuns(t, NewMemory())
}

func TestPgProjectStageRuns(t *testing.T) {
	checkProjectStageRuns(t, testPool(t))
}

// exactAggBase 受控时间戳基准：StartStageRun/FinishStageRun 的 now() 造不出
// 确定耗时，精确断言（avg/p95/duration/窗口截取）直喂底层存储。
var exactAggBase = time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)

// seedExactAggregationMemory 直喂内部存储：spec 5 条已收尾（耗时 100..500ms，
// started_at 逐秒递增）+ code_gen 1 条 running（最后启动）。
func seedExactAggregationMemory(t *testing.T, m *Memory, dID string) {
	t.Helper()
	runs := make([]*StageRun, 0, 6)
	for i := 0; i < 5; i++ {
		start := exactAggBase.Add(time.Duration(i) * time.Second)
		fin := start.Add(time.Duration(100*(i+1)) * time.Millisecond)
		runs = append(runs, &StageRun{
			ID:         fmt.Sprintf("aaaa%04d-0000-0000-0000-000000000001", i+1),
			DeliveryID: dID, Stage: "spec", Attempt: i + 1, Status: "done",
			StartedAt: start, FinishedAt: &fin,
		})
	}
	runs = append(runs, &StageRun{
		ID:         "aaaa0006-0000-0000-0000-000000000001",
		DeliveryID: dID, Stage: "code_gen", Attempt: 1, Status: "running",
		StartedAt: exactAggBase.Add(10 * time.Second),
	})
	m.stageRuns[dID] = runs
}

// seedExactAggregationPg 裸 SQL 同构铺数（绕过 StartStageRun 的 DB 默认 now()）。
func seedExactAggregationPg(t *testing.T, p *Pg, dID string) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		start := exactAggBase.Add(time.Duration(i) * time.Second)
		fin := start.Add(time.Duration(100*(i+1)) * time.Millisecond)
		_, err := p.pool.Exec(ctx,
			`INSERT INTO stage_runs (id,delivery_id,stage,attempt,status,started_at,finished_at) VALUES ($1,$2,'spec',$3,'done',$4,$5)`,
			fmt.Sprintf("aaaa%04d-0000-0000-0000-000000000001", i+1), dID, i+1, start, fin)
		require.NoError(t, err)
	}
	_, err := p.pool.Exec(ctx,
		`INSERT INTO stage_runs (id,delivery_id,stage,attempt,status,started_at) VALUES ('aaaa0006-0000-0000-0000-000000000001',$1,'code_gen',1,'running',$2)`,
		dID, exactAggBase.Add(10*time.Second))
	require.NoError(t, err)
}

// checkStageRunsAggregation 精确断言：明细序（最新在前）+ duration 精确值 +
// avg/p95 口径（只统计已收尾；p95 最近邻位法）+ running 不进耗时统计。
func checkStageRunsAggregation(t *testing.T, st Store, projectID, dID string) {
	t.Helper()
	got, err := st.ProjectStageRuns(context.Background(), projectID)
	require.NoError(t, err)
	require.Len(t, got.Runs, 6)

	// started_at 倒序：code_gen running（+10s）最先，随后 spec attempt 5..1。
	require.Equal(t, "code_gen", got.Runs[0].Stage)
	require.Equal(t, "running", got.Runs[0].Status)
	require.Nil(t, got.Runs[0].DurationMS)
	for i := 0; i < 5; i++ {
		require.Equal(t, 5-i, got.Runs[1+i].Attempt, "第 %d 行应为 spec attempt %d", i+1, 5-i)
	}
	require.Equal(t, int64(500), *got.Runs[1].DurationMS, "attempt 5 耗时 500ms")
	require.Equal(t, int64(100), *got.Runs[5].DurationMS, "attempt 1 耗时 100ms")

	byStage := map[string]StageRunStageStats{}
	for _, s := range got.ByStage {
		byStage[s.Stage] = s
	}
	require.Equal(t, 5, byStage["spec"].Total)
	require.Equal(t, 5, byStage["spec"].Done)
	require.Equal(t, 0, byStage["spec"].Failed)
	require.Equal(t, float64(300), byStage["spec"].AvgMS, "(100+200+300+400+500)/5 = 300")
	require.Equal(t, float64(500), byStage["spec"].P95MS, "最近邻位法 ceil(0.95×5)=5 → 第 5 个值")
	require.Equal(t, 1, byStage["code_gen"].Total)
	require.Equal(t, 1, byStage["code_gen"].Running)
	require.Equal(t, float64(0), byStage["code_gen"].AvgMS, "无已收尾运行 → 0 而非 NaN")
	require.Equal(t, float64(0), byStage["code_gen"].P95MS)
}

func TestMemoryProjectStageRunsAggregation(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	proj := &Project{Name: "聚合项目"}
	require.NoError(t, m.CreateProject(ctx, proj))
	d := &Delivery{ProjectID: proj.ID, Title: "需求", Status: "active"}
	require.NoError(t, m.CreateDelivery(ctx, d))
	seedExactAggregationMemory(t, m, d.ID)
	checkStageRunsAggregation(t, m, proj.ID, d.ID)
}

func TestPgProjectStageRunsAggregation(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	proj := &Project{Name: "聚合项目"}
	require.NoError(t, p.CreateProject(ctx, proj))
	d := &Delivery{ProjectID: proj.ID, Title: "需求", Status: "active"}
	require.NoError(t, p.CreateDelivery(ctx, d))
	seedExactAggregationPg(t, p, d.ID)
	checkStageRunsAggregation(t, p, proj.ID, d.ID)
}

// seedWindowLimitMemory 铺 205 条 test_gen 运行（started_at 逐秒递增、全部
// 已收尾），超出 stageRunsDetailLimit=200 的窗口。
func seedWindowLimitMemory(t *testing.T, m *Memory, dID string) {
	t.Helper()
	runs := make([]*StageRun, 0, 205)
	for i := 0; i < 205; i++ {
		start := exactAggBase.Add(time.Duration(i) * time.Second)
		fin := start.Add(10 * time.Millisecond)
		runs = append(runs, &StageRun{
			ID:         fmt.Sprintf("bbbb%04d-0000-0000-0000-000000000001", i+1),
			DeliveryID: dID, Stage: "test_gen", Attempt: i + 1, Status: "done",
			StartedAt: start, FinishedAt: &fin,
		})
	}
	m.stageRuns[dID] = runs
}

func seedWindowLimitPg(t *testing.T, p *Pg, dID string) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 205; i++ {
		start := exactAggBase.Add(time.Duration(i) * time.Second)
		_, err := p.pool.Exec(ctx,
			`INSERT INTO stage_runs (id,delivery_id,stage,attempt,status,started_at,finished_at) VALUES ($1,$2,'test_gen',$3,'done',$4,$5)`,
			fmt.Sprintf("bbbb%04d-0000-0000-0000-000000000001", i+1), dID, i+1, start, start.Add(10*time.Millisecond))
		require.NoError(t, err)
	}
}

// checkStageRunsWindowLimit 窗口有界：明细只留最近 200 条（attempt 205..6），
// by_stage 聚合同一窗口（Total=200，不数被截掉的 5 条旧运行）。
func checkStageRunsWindowLimit(t *testing.T, st Store, projectID string) {
	t.Helper()
	got, err := st.ProjectStageRuns(context.Background(), projectID)
	require.NoError(t, err)
	require.Len(t, got.Runs, stageRunsDetailLimit)
	require.Equal(t, 205, got.Runs[0].Attempt, "最新（attempt 205）在前")
	require.Equal(t, 6, got.Runs[stageRunsDetailLimit-1].Attempt, "窗口只留最近 200 条：attempt 6 垫底，1..5 被截掉")
	require.Equal(t, stageRunsDetailLimit, got.ByStage[0].Total, "聚合同一窗口")
	require.Equal(t, stageRunsDetailLimit, got.ByStage[0].Done)
	require.Equal(t, float64(10), got.ByStage[0].AvgMS)
}

func TestMemoryProjectStageRunsWindowLimit(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	proj := &Project{Name: "窗口项目"}
	require.NoError(t, m.CreateProject(ctx, proj))
	d := &Delivery{ProjectID: proj.ID, Title: "需求", Status: "active"}
	require.NoError(t, m.CreateDelivery(ctx, d))
	seedWindowLimitMemory(t, m, d.ID)
	checkStageRunsWindowLimit(t, m, proj.ID)
}

func TestPgProjectStageRunsWindowLimit(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	proj := &Project{Name: "窗口项目"}
	require.NoError(t, p.CreateProject(ctx, proj))
	d := &Delivery{ProjectID: proj.ID, Title: "需求", Status: "active"}
	require.NoError(t, p.CreateDelivery(ctx, d))
	seedWindowLimitPg(t, p, d.ID)
	checkStageRunsWindowLimit(t, p, proj.ID)
}
