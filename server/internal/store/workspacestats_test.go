package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// workspaceStatsBase 受控时间戳基准：窗口/分桶边界全部由它推出，精确断言
// 窗口边界、跨小时归桶与时区换算。
var workspaceStatsBase = time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)

// workspaceStatsRun 铺数行（跨实现同构）：窗口 [base, base+24h)。
//   - w1 base(10:00) done 30m、w2 base+29m59s done 15m1s → 10 点桶 2 次
//   - w3 base+12h15m(22:15) failed 30m → 22 点桶
//   - w4 base+13h(23:00) done 90m（跨小时收尾，整段计入起始桶）→ 23 点桶
//   - w5 base+19h(次日 05:00) running 未收尾 → 5 点桶计次不计时长
//   - w6 base-1s / w7 base+24h → 窗外剔除（from 闭、to 开）
type workspaceStatsRun struct {
	id       string
	delivery int // 0 → d1（主项目）、1 → d2（别项目）
	stage    string
	status   string
	started  time.Time
	finished *time.Time
}

func workspaceStatsSeedRuns(d1, d2 string) []workspaceStatsRun {
	fin := func(t time.Time) *time.Time { return &t }
	return []workspaceStatsRun{
		{"aaaaa001-0000-0000-0000-000000000001", 0, "spec", "done", workspaceStatsBase, fin(workspaceStatsBase.Add(30 * time.Minute))},
		{"aaaaa002-0000-0000-0000-000000000002", 0, "code_gen", "done", workspaceStatsBase.Add(29*time.Minute + 59*time.Second), fin(workspaceStatsBase.Add(45 * time.Minute))},
		{"aaaaa003-0000-0000-0000-000000000003", 1, "spec", "failed", workspaceStatsBase.Add(12*time.Hour + 15*time.Minute), fin(workspaceStatsBase.Add(12*time.Hour + 45*time.Minute))},
		{"aaaaa004-0000-0000-0000-000000000004", 1, "code_gen", "done", workspaceStatsBase.Add(13 * time.Hour), fin(workspaceStatsBase.Add(14*time.Hour + 30*time.Minute))},
		{"aaaaa005-0000-0000-0000-000000000005", 0, "spec", "running", workspaceStatsBase.Add(19 * time.Hour), nil},
		{"aaaaa006-0000-0000-0000-000000000006", 1, "spec", "done", workspaceStatsBase.Add(-1 * time.Second), fin(workspaceStatsBase.Add(time.Minute))},
		{"aaaaa007-0000-0000-0000-000000000007", 1, "spec", "done", workspaceStatsBase.Add(24 * time.Hour), fin(workspaceStatsBase.Add(24*time.Hour + 5*time.Minute))},
	}
}

// seedWorkspaceStatsCommon 公共面铺数：两项目 + 七条需求铺满五种状态
// （completed×2、active×2、queued×1、blocked×1、cancelled×1）。
func seedWorkspaceStatsCommon(t *testing.T, st Store) (d1, d2 string) {
	t.Helper()
	ctx := context.Background()
	main := &Project{Name: "统计主项目"}
	require.NoError(t, st.CreateProject(ctx, main))
	other := &Project{Name: "统计别项目"}
	require.NoError(t, st.CreateProject(ctx, other))
	for i, status := range []string{"completed", "active", "queued", "blocked", "cancelled", "active", "completed"} {
		d := &Delivery{ProjectID: main.ID, Title: "需求", Status: status}
		if i >= 5 {
			d.ProjectID = other.ID
		}
		require.NoError(t, st.CreateDelivery(ctx, d))
		if i == 0 {
			d1 = d.ID
		}
		if i == 5 {
			d2 = d.ID
		}
	}
	return d1, d2
}

// seedWorkspaceStatsMemory 直喂内部存储：started_at/finished_at 精确可控
// （StartStageRun 会覆盖 started_at 为 now()）。
func seedWorkspaceStatsMemory(t *testing.T, m *Memory) {
	t.Helper()
	d1, d2 := seedWorkspaceStatsCommon(t, m)
	for _, s := range workspaceStatsSeedRuns(d1, d2) {
		delivery := d1
		if s.delivery == 1 {
			delivery = d2
		}
		m.stageRuns[delivery] = append(m.stageRuns[delivery], &StageRun{
			ID: s.id, DeliveryID: delivery, Stage: s.stage, Attempt: 1,
			Status: s.status, StartedAt: s.started, FinishedAt: s.finished,
		})
	}
}

// seedWorkspaceStatsPg 裸 SQL 同构铺数（绕过 StartStageRun 的 DB 默认 now()）。
func seedWorkspaceStatsPg(t *testing.T, p *Pg, d1, d2 string) {
	t.Helper()
	ctx := context.Background()
	for _, s := range workspaceStatsSeedRuns(d1, d2) {
		delivery := d1
		if s.delivery == 1 {
			delivery = d2
		}
		_, err := p.pool.Exec(ctx,
			`INSERT INTO stage_runs (id,delivery_id,stage,attempt,status,started_at,finished_at) VALUES ($1,$2,$3,1,$4,$5,$6)`,
			s.id, delivery, s.stage, s.status, s.started, s.finished)
		require.NoError(t, err)
	}
}

// checkWorkspaceStats 精确断言（窗口 [base, base+24h)）：
// 状态分布（全量快照 + 五类归并 + 原始状态计数）、窗口边界（from 闭 to 开）、
// 执行计数含全部状态、时长只累计已收尾、跨小时整段计入起始桶、
// 逐小时补零 24 桶、时区换算、未知状态防御性归桶。
func checkWorkspaceStats(t *testing.T, st Store) {
	t.Helper()
	ctx := context.Background()
	from := workspaceStatsBase
	to := workspaceStatsBase.Add(24 * time.Hour)

	got, err := st.WorkspaceStats(ctx, from, to, time.UTC)
	require.NoError(t, err)

	// 状态分布：全量快照（不受窗口影响），七条需求跨两项目。
	require.Equal(t, 7, got.TaskStatus.Total)
	require.Equal(t, 2, got.TaskStatus.Done, "completed → 已完成")
	require.Equal(t, 2, got.TaskStatus.InProgress, "active → 进行中")
	require.Equal(t, 2, got.TaskStatus.Todo, "queued+blocked → 待办")
	require.Equal(t, 1, got.TaskStatus.Cancelled, "cancelled → 已取消")
	require.Equal(t, map[string]int{"active": 2, "queued": 1, "completed": 2, "blocked": 1, "cancelled": 1},
		got.TaskStatus.ByStatus, "原始状态计数恒含五键")

	// 执行统计：窗口内 5 次（w6/w7 窗外剔除），计数不分状态，时长只计已收尾。
	require.Equal(t, 5, got.Execution.RunsTotal)
	require.Equal(t, 3, got.Execution.Done)
	require.Equal(t, 1, got.Execution.Failed)
	require.Equal(t, 1, got.Execution.Running)
	require.Equal(t, int64((30*time.Minute+15*time.Minute+time.Second+30*time.Minute+90*time.Minute)/time.Millisecond),
		got.Execution.DurationMSTotal, "running 不计时长，时长单位毫秒")

	// 逐小时分桶：恒 24 桶按 hour 升序补零。
	require.Len(t, got.Hourly, 24)
	for i, b := range got.Hourly {
		require.Equal(t, i, b.Hour, "第 %d 桶 hour=%d", i, i)
	}
	byHour := map[int]WorkspaceHourBucket{}
	for _, b := range got.Hourly {
		byHour[b.Hour] = b
	}
	require.Equal(t, 2, byHour[10].Runs, "10 点桶：w1+w2")
	require.Equal(t, int64((30*time.Minute+15*time.Minute+time.Second)/time.Millisecond), byHour[10].DurationMS)
	require.Equal(t, 1, byHour[22].Runs)
	require.Equal(t, int64(30*time.Minute/time.Millisecond), byHour[22].DurationMS)
	require.Equal(t, 1, byHour[23].Runs)
	require.Equal(t, int64(90*time.Minute/time.Millisecond), byHour[23].DurationMS, "23:00 收尾于 00:30 的执行整段计入 23 点桶")
	require.Equal(t, 1, byHour[5].Runs, "running 也计次")
	require.Equal(t, int64(0), byHour[5].DurationMS, "running 不计时长")
	for _, h := range []int{0, 1, 2, 3, 4, 6, 7, 8, 9, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21} {
		require.Zero(t, byHour[h].Runs, "%d 点桶补零", h)
		require.Zero(t, byHour[h].DurationMS)
	}

	// 时区换算：同一批数据按 Asia/Shanghai（UTC+8）归桶，桶号随之下移。
	sh, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	got, err = st.WorkspaceStats(ctx, from, to, sh)
	require.NoError(t, err)
	require.Equal(t, 5, got.Execution.RunsTotal, "时区不影响计数")
	require.Len(t, got.Hourly, 24)
	byHour = map[int]WorkspaceHourBucket{}
	for _, b := range got.Hourly {
		byHour[b.Hour] = b
	}
	require.Equal(t, 2, byHour[18].Runs, "10 点 UTC → 18 点上海")
	require.Equal(t, 1, byHour[6].Runs, "22:15 UTC → 次日 06:15 上海")
	require.Equal(t, int64(30*time.Minute/time.Millisecond), byHour[6].DurationMS)
	require.Equal(t, 1, byHour[7].Runs, "23:00 UTC → 次日 07:00 上海")
	require.Equal(t, 1, byHour[13].Runs, "05:00 UTC → 13:00 上海")

	// loc nil → UTC（缺省口径）。
	got, err = st.WorkspaceStats(ctx, from, to, nil)
	require.NoError(t, err)
	require.Equal(t, 2, got.Hourly[10].Runs)
}

// checkWorkspaceStatsEmpty 空库：状态分布与执行统计全零、hourly 为 24 个零桶
// （非空数组）。
func checkWorkspaceStatsEmpty(t *testing.T, st Store) {
	t.Helper()
	got, err := st.WorkspaceStats(context.Background(), workspaceStatsBase, workspaceStatsBase.Add(24*time.Hour), time.UTC)
	require.NoError(t, err)
	require.Equal(t, WorkspaceTaskStatus{
		ByStatus: map[string]int{"active": 0, "queued": 0, "completed": 0, "blocked": 0, "cancelled": 0},
	}, got.TaskStatus)
	require.Equal(t, WorkspaceExecution{}, got.Execution)
	require.Len(t, got.Hourly, 24)
	for _, b := range got.Hourly {
		require.Zero(t, b.Runs)
		require.Zero(t, b.DurationMS)
	}
}

func TestWorkspaceStatsMemory(t *testing.T) {
	m := NewMemory()
	seedWorkspaceStatsMemory(t, m)
	checkWorkspaceStats(t, m)
}

func TestWorkspaceStatsEmptyMemory(t *testing.T) {
	checkWorkspaceStatsEmpty(t, NewMemory())
}

func TestWorkspaceStatsInvalidWindow(t *testing.T) {
	m := NewMemory()
	base := workspaceStatsBase
	for _, tc := range [][2]time.Time{{base, base}, {base, base.Add(-time.Hour)}} {
		_, err := m.WorkspaceStats(context.Background(), tc[0], tc[1], time.UTC)
		require.ErrorIs(t, err, ErrInvalid, "窗口非正 → ErrInvalid（from=%v to=%v）", tc[0], tc[1])
	}
}

func TestWorkspaceStatsPg(t *testing.T) {
	p := testPool(t)
	d1, d2 := seedWorkspaceStatsCommon(t, p)
	seedWorkspaceStatsPg(t, p, d1, d2)
	checkWorkspaceStats(t, p)
}

func TestWorkspaceStatsEmptyPg(t *testing.T) {
	checkWorkspaceStatsEmpty(t, testPool(t))
}

func TestWorkspaceStatsPgInvalidWindow(t *testing.T) {
	p := testPool(t)
	_, err := p.WorkspaceStats(context.Background(), workspaceStatsBase, workspaceStatsBase, time.UTC)
	require.ErrorIs(t, err, ErrInvalid)
}
