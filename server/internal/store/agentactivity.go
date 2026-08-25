package store

import (
	"slices"
	"strings"
	"time"
)

// 本文件是 AgentActivity（INFERA-253）的共用装配逻辑：分桶在 pg/memory
// 两个实现间共享同一份代码，桶边界与补零口径天然一致。

// unboundAgentName 无绑定 stage 的运行归组名（INFERA-253 冻结契约）：该曲线
// agent_id 为空串，与真实 agent（name 唯一、id 非空）区分。
const unboundAgentName = "unbound"

// agentActivityRow 聚合查询的原始行：窗口内一次 stage_run 的归属 agent 与
// 启动时间。AgentID 空串 = 无绑定（unbound），AgentName 已解析为最终展示名。
type agentActivityRow struct {
	AgentID   string
	AgentName string
	StartedAt time.Time
}

// assembleAgentActivity 分桶装配：桶自 from 起对齐铺满 [from,to)——started_at
// 恰落桶起点归该桶（半开区间左闭右开），attempt 各计一次、不分 status；每条
// 曲线覆盖窗口内全部桶（含 count=0）等长对齐；输出按 agent_name 升序（并列
// 按 agent_id 稳定）。桶宽非正或窗口非正 → ErrInvalid。
func assembleAgentActivity(rows []agentActivityRow, from, to time.Time, bucketMinutes int) ([]AgentActivitySeries, error) {
	if bucketMinutes <= 0 || !from.Before(to) {
		return nil, ErrInvalid
	}
	bucket := time.Duration(bucketMinutes) * time.Minute
	window := to.Sub(from)
	buckets := int((window + bucket - 1) / bucket)

	byAgent := map[string]*AgentActivitySeries{}
	for _, r := range rows {
		offset := r.StartedAt.Sub(from)
		if offset < 0 || offset >= window {
			continue // 窗外行（查询层已过滤，防御性兜底）
		}
		s, ok := byAgent[r.AgentID]
		if !ok {
			s = &AgentActivitySeries{AgentID: r.AgentID, AgentName: r.AgentName, Points: make([]AgentActivityPoint, buckets)}
			for i := range s.Points {
				s.Points[i].T = from.Add(time.Duration(i) * bucket)
			}
			byAgent[r.AgentID] = s
		}
		s.Points[offset/bucket].Count++
	}

	out := make([]AgentActivitySeries, 0, len(byAgent))
	for _, s := range byAgent {
		out = append(out, *s)
	}
	slices.SortFunc(out, func(a, b AgentActivitySeries) int {
		if c := strings.Compare(a.AgentName, b.AgentName); c != 0 {
			return c
		}
		return strings.Compare(a.AgentID, b.AgentID)
	})
	return out, nil
}
