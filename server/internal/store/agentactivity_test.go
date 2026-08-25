package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// agentActivityBase 受控时间戳基准：窗口/桶边界全部由它推出，精确断言分桶。
var agentActivityBase = time.Date(2026, 8, 25, 8, 0, 0, 0, time.UTC)

// agent-activity 固定 ID（两实现共用，期望值免回查）。
const (
	agentActivityAlphaID = "aaaaa001-0000-0000-0000-000000000001"
	agentActivityBetaID  = "aaaaa002-0000-0000-0000-000000000002"
)

// agentActivityRuns 精确铺数（跨实现同构）：窗口 [base, base+2h)、桶 30m。
//   - d1（主项目）spec：base、base+29m59s → Beta 桶0；base+30m → Beta 桶1
//     （主项目绑定优先于全局）；base-1s / base+2h / base+2h+1s → 窗外剔除
//   - d2（别项目）spec：base+45m → Alpha 桶1（无项目绑定 → 全局兜底）
//   - d1 code_gen：base+61m / base+90m → unbound（无任何绑定）
type agentActivityRun struct {
	id      string
	stage   string
	started time.Time
}

var agentActivitySeed = []agentActivityRun{
	{"ccccc001-0000-0000-0000-000000000001", "spec", agentActivityBase},
	{"ccccc002-0000-0000-0000-000000000001", "spec", agentActivityBase.Add(29*time.Minute + 59*time.Second)},
	{"ccccc003-0000-0000-0000-000000000001", "spec", agentActivityBase.Add(30 * time.Minute)},
	{"ccccc004-0000-0000-0000-000000000001", "spec", agentActivityBase.Add(45 * time.Minute)},
	{"ccccc005-0000-0000-0000-000000000001", "code_gen", agentActivityBase.Add(61 * time.Minute)},
	{"ccccc006-0000-0000-0000-000000000001", "code_gen", agentActivityBase.Add(90 * time.Minute)},
	{"ccccc007-0000-0000-0000-000000000001", "spec", agentActivityBase.Add(-1 * time.Second)},
	{"ccccc008-0000-0000-0000-000000000001", "spec", agentActivityBase.Add(2 * time.Hour)},
	{"ccccc009-0000-0000-0000-000000000001", "spec", agentActivityBase.Add(2*time.Hour + 1*time.Second)},
}

// seedAgentActivityCommon 公共面铺数：两项目、两 agent（Alpha 全局绑 spec、
// Beta 绑主项目 spec）、两需求。全局绑定不走 UpsertBinding（项目级专用），
// 由各实现的 seed 函数自行落。
func seedAgentActivityCommon(t *testing.T, st Store) (mainID, otherID, d1, d2 string) {
	t.Helper()
	ctx := context.Background()
	main := &Project{Name: "时序主项目"}
	require.NoError(t, st.CreateProject(ctx, main))
	other := &Project{Name: "时序别项目"}
	require.NoError(t, st.CreateProject(ctx, other))

	alpha := &Agent{ID: agentActivityAlphaID, Name: "Alpha", Runner: "local"}
	require.NoError(t, st.CreateAgent(ctx, alpha))
	beta := &Agent{ID: agentActivityBetaID, Name: "Beta", Runner: "local"}
	require.NoError(t, st.CreateAgent(ctx, beta))
	require.NoError(t, st.UpsertBinding(ctx, &PipelineBinding{ProjectID: main.ID, Node: "spec", AgentID: beta.ID}))

	del1 := &Delivery{ProjectID: main.ID, Title: "需求一", Status: "active"}
	require.NoError(t, st.CreateDelivery(ctx, del1))
	del2 := &Delivery{ProjectID: other.ID, Title: "需求二", Status: "active"}
	require.NoError(t, st.CreateDelivery(ctx, del2))
	return main.ID, other.ID, del1.ID, del2.ID
}

// seedAgentActivityMemory 直喂内部存储：started_at 精确可控 + 全局绑定
// （bindingKey("", node)，UpsertBinding 拒绝空 projectID）。
func seedAgentActivityMemory(t *testing.T, m *Memory) {
	t.Helper()
	_, _, d1, d2 := seedAgentActivityCommon(t, m)
	m.bindings[bindingKey("", "spec")] = &PipelineBinding{
		ID: "bbbbb001-0000-0000-0000-000000000001", Node: "spec", AgentID: agentActivityAlphaID,
	}
	for i, s := range agentActivitySeed {
		// 第 4 条（base+45m）属 d2：别项目的 spec 验证全局兜底。
		delivery, id := d1, s.id
		if i == 3 {
			delivery, id = d2, fmt.Sprintf("ddddd%03d-0000-0000-0000-000000000001", i+1)
		}
		m.stageRuns[delivery] = append(m.stageRuns[delivery], &StageRun{
			ID: id, DeliveryID: delivery, Stage: s.stage, Attempt: 1, Status: "done", StartedAt: s.started,
		})
	}
}

// seedAgentActivityPg 裸 SQL 同构铺数（绕过 StartStageRun 的 DB 默认 now()，
// 并落一条 project_id IS NULL 的全局绑定）。
func seedAgentActivityPg(t *testing.T, p *Pg, d1, d2 string) {
	t.Helper()
	ctx := context.Background()
	_, err := p.pool.Exec(ctx,
		`INSERT INTO pipeline_bindings (id,project_id,node,agent_id) VALUES ($1,NULL,'spec',$2)`,
		"bbbbb001-0000-0000-0000-000000000001", agentActivityAlphaID)
	require.NoError(t, err)
	for i, s := range agentActivitySeed {
		delivery, id := d1, s.id
		if i == 3 {
			delivery, id = d2, fmt.Sprintf("ddddd%03d-0000-0000-0000-000000000001", i+1)
		}
		_, err := p.pool.Exec(ctx,
			`INSERT INTO stage_runs (id,delivery_id,stage,attempt,status,started_at) VALUES ($1,$2,$3,1,'done',$4)`,
			id, delivery, s.stage, s.started)
		require.NoError(t, err)
	}
}

// countsOf 取某条曲线的全桶计数（按桶序）。
func countsOf(t *testing.T, s AgentActivitySeries) []int {
	t.Helper()
	out := make([]int, 0, len(s.Points))
	for _, p := range s.Points {
		out = append(out, p.Count)
	}
	return out
}

// checkAgentActivity 精确断言（窗口 [base, base+2h)）：
// 分桶边界（桶起点对齐、started_at==桶起点归该桶）、跨桶计数、零桶补齐、
// 曲线等长、agent 解析（项目优先 / 全局兜底 / unbound）、series 按 name 排序。
func checkAgentActivity(t *testing.T, st Store) {
	t.Helper()
	ctx := context.Background()
	from := agentActivityBase
	to := agentActivityBase.Add(2 * time.Hour)

	got, err := st.AgentActivity(ctx, from, to, 30)
	require.NoError(t, err)
	require.Len(t, got, 3, "窗口内有执行的 agent：Alpha / Beta / unbound")
	require.Equal(t, "Alpha", got[0].AgentName, "series 按 agent_name 排序")
	require.Equal(t, agentActivityAlphaID, got[0].AgentID)
	require.Equal(t, "Beta", got[1].AgentName)
	require.Equal(t, agentActivityBetaID, got[1].AgentID)
	require.Equal(t, "unbound", got[2].AgentName, "无绑定 → unbound 分组")
	require.Empty(t, got[2].AgentID, "unbound 无真实 agent，agent_id 为空串")

	// 桶起点对齐：4 桶 × 30m，t 依次为 base、+30m、+60m、+90m。
	for _, s := range got {
		require.Len(t, s.Points, 4, "%s 覆盖窗口内全部桶（含 count=0）", s.AgentName)
		for i, p := range s.Points {
			require.Equal(t, from.Add(time.Duration(i)*30*time.Minute), p.T, "%s 第 %d 桶起点", s.AgentName, i)
		}
	}
	require.Equal(t, []int{0, 1, 0, 0}, countsOf(t, got[0]), "Alpha：base+45m 落桶1（全局兜底）")
	require.Equal(t, []int{2, 1, 0, 0}, countsOf(t, got[1]), "Beta：base 与 +29m59s 同桶0，+30m 起始新桶1")
	require.Equal(t, []int{0, 0, 1, 1}, countsOf(t, got[2]), "unbound：code_gen +61m/+90m 分落桶2/桶3")

	// 换桶宽（60m）：同一窗口 2 桶，口径随 bucket_minutes 变化（+45m 归桶0，
	// +30m 与 base 同桶）。
	got60, err := st.AgentActivity(ctx, from, to, 60)
	require.NoError(t, err)
	require.Len(t, got60, 3)
	require.Equal(t, []int{1, 0}, countsOf(t, got60[0]))
	require.Equal(t, []int{3, 0}, countsOf(t, got60[1]))
	require.Equal(t, []int{0, 2}, countsOf(t, got60[2]))
	for i, p := range got60[0].Points {
		require.Equal(t, from.Add(time.Duration(i)*time.Hour), p.T)
	}
}

func TestMemoryAgentActivity(t *testing.T) {
	m := NewMemory()
	seedAgentActivityMemory(t, m)
	checkAgentActivity(t, m)
}

func TestPgAgentActivity(t *testing.T) {
	p := testPool(t)
	_, _, d1, d2 := seedAgentActivityCommon(t, p)
	seedAgentActivityPg(t, p, d1, d2)
	checkAgentActivity(t, p)
}

// checkAgentActivityEmpty 空窗口：无任何执行 → series 为空数组（非 nil）、无错。
func checkAgentActivityEmpty(t *testing.T, st Store) {
	t.Helper()
	ctx := context.Background()
	got, err := st.AgentActivity(ctx, agentActivityBase, agentActivityBase.Add(time.Hour), 30)
	require.NoError(t, err)
	require.NotNil(t, got, "空窗口是 [] 不是 null")
	require.Empty(t, got)
}

func TestMemoryAgentActivityEmpty(t *testing.T) {
	checkAgentActivityEmpty(t, NewMemory())
}

func TestPgAgentActivityEmpty(t *testing.T) {
	checkAgentActivityEmpty(t, testPool(t))
}

// checkAgentActivityInvalid 非法参数：桶宽非正、窗口非正（to <= from）→ ErrInvalid。
func checkAgentActivityInvalid(t *testing.T, st Store) {
	t.Helper()
	ctx := context.Background()
	_, err := st.AgentActivity(ctx, agentActivityBase, agentActivityBase.Add(time.Hour), 0)
	require.ErrorIs(t, err, ErrInvalid)
	_, err = st.AgentActivity(ctx, agentActivityBase, agentActivityBase.Add(time.Hour), -30)
	require.ErrorIs(t, err, ErrInvalid)
	_, err = st.AgentActivity(ctx, agentActivityBase, agentActivityBase, 30)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestMemoryAgentActivityInvalid(t *testing.T) {
	checkAgentActivityInvalid(t, NewMemory())
}

func TestPgAgentActivityInvalid(t *testing.T) {
	checkAgentActivityInvalid(t, testPool(t))
}
