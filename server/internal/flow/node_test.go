package flow

import "testing"

// TestNodeFromExternalStatus：上游父 issue 状态 → 大节点映射全表。
// 只有四个业务状态有映射；blocked / backlog / cancelled / 未知 / 大小写变体
// 一律无映射（不推进节点）。
func TestNodeFromExternalStatus(t *testing.T) {
	cases := []struct {
		status string
		want   Node
		ok     bool
	}{
		{"todo", NodeDispatched, true},
		{"in_progress", NodeInProgress, true},
		{"in_review", NodeInReview, true},
		{"done", NodeDelivered, true},
		// 不推进的状态（blocked 及未知）
		{"blocked", "", false},
		{"backlog", "", false},
		{"cancelled", "", false},
		{"", "", false},
		{"TODO", "", false}, // 字面协议：大小写敏感
		{"in-progress", "", false},
	}
	for _, tc := range cases {
		got, ok := NodeFromExternalStatus(tc.status)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("NodeFromExternalStatus(%q) = (%q, %v), want (%q, %v)",
				tc.status, got, ok, tc.want, tc.ok)
		}
	}
}

// TestCanTransition：合法跃迁全矩阵。
// 规则：主链 intake→dispatched→in_progress→in_review→delivered 只进不退
// （允许跨级——轮询可能错过中间状态）；needs_decision 从任意活跃节点进入、
// 可回到任意活跃节点或 delivered；delivered 是终态；自跃迁不算合法跃迁。
func TestCanTransition(t *testing.T) {
	cases := []struct {
		from, to Node
		want     bool
	}{
		// 主链前进（含跨级：一次轮询窗口内状态可能连跳）
		{NodeIntake, NodeDispatched, true},
		{NodeIntake, NodeInProgress, true},
		{NodeIntake, NodeInReview, true},
		{NodeIntake, NodeDelivered, true},
		{NodeDispatched, NodeInProgress, true},
		{NodeDispatched, NodeInReview, true},
		{NodeDispatched, NodeDelivered, true},
		{NodeInProgress, NodeInReview, true},
		{NodeInProgress, NodeDelivered, true},
		{NodeInReview, NodeDelivered, true},
		// 主链后退一律非法（单一状态源：infera 不因上游回退而回退）
		{NodeDispatched, NodeIntake, false},
		{NodeInProgress, NodeDispatched, false},
		{NodeInProgress, NodeIntake, false},
		{NodeInReview, NodeInProgress, false},
		{NodeInReview, NodeDispatched, false},
		{NodeDelivered, NodeInReview, false},
		{NodeDelivered, NodeInProgress, false},
		{NodeDelivered, NodeDispatched, false},
		{NodeDelivered, NodeIntake, false},
		// 需决策：活跃节点可进入，可回到活跃节点或直达已交付
		{NodeDispatched, NodeNeedsDecision, true},
		{NodeInProgress, NodeNeedsDecision, true},
		{NodeInReview, NodeNeedsDecision, true},
		{NodeNeedsDecision, NodeDispatched, true},
		{NodeNeedsDecision, NodeInProgress, true},
		{NodeNeedsDecision, NodeInReview, true},
		{NodeNeedsDecision, NodeDelivered, true},
		// 受理前无 issue 无闸门；交付后流程已终
		{NodeIntake, NodeNeedsDecision, false},
		{NodeDelivered, NodeNeedsDecision, false},
		{NodeNeedsDecision, NodeIntake, false},
		// 自跃迁不是跃迁
		{NodeIntake, NodeIntake, false},
		{NodeDispatched, NodeDispatched, false},
		{NodeNeedsDecision, NodeNeedsDecision, false},
	}
	for _, tc := range cases {
		if got := CanTransition(tc.from, tc.to); got != tc.want {
			t.Fatalf("CanTransition(%s, %s) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

// TestAdvance：状态映射 + 跃迁合法性的组合推进。
// 不可映射（blocked/未知）、非法后退、目标等于当前——一律保持原节点。
func TestAdvance(t *testing.T) {
	cases := []struct {
		name    string
		current Node
		status  string
		want    Node
	}{
		{"正常推进", NodeDispatched, "in_progress", NodeInProgress},
		{"跨级推进（错过中间状态）", NodeDispatched, "in_review", NodeInReview},
		{"待验收到已交付", NodeInReview, "done", NodeDelivered},
		{"blocked 不推进", NodeInProgress, "blocked", NodeInProgress},
		{"未知状态不推进", NodeInProgress, "weird", NodeInProgress},
		{"空状态不推进", NodeInProgress, "", NodeInProgress},
		{"后退拒绝", NodeInReview, "todo", NodeInReview},
		{"同节点不动", NodeInProgress, "in_progress", NodeInProgress},
		{"决策节点随状态回主链", NodeNeedsDecision, "in_review", NodeInReview},
		{"决策节点不因 blocked 弹回", NodeNeedsDecision, "blocked", NodeNeedsDecision},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Advance(tc.current, tc.status); got != tc.want {
				t.Fatalf("Advance(%s, %q) = %s, want %s", tc.current, tc.status, got, tc.want)
			}
		})
	}
}
