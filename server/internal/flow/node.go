package flow

// Node 是需求在 infera 侧的业务大节点（单一状态源）。
// 值为稳定 slug，落库 TEXT，前端负责渲染中文标签。
type Node string

const (
	NodeIntake        Node = "intake"         // 需求受理（未建卡）
	NodeDispatched    Node = "dispatched"     // 已派发
	NodeInProgress    Node = "in_progress"    // 执行中
	NodeInReview      Node = "in_review"      // 待验收
	NodeDelivered     Node = "delivered"      // 已交付
	NodeNeedsDecision Node = "needs_decision" // ⚠️ 需决策（异常节点，闸门检测触发）
)

// chain 是主链顺序（不含 needs_decision）。
var chain = []Node{NodeIntake, NodeDispatched, NodeInProgress, NodeInReview, NodeDelivered}

func chainIndex(n Node) int {
	for i, c := range chain {
		if c == n {
			return i
		}
	}
	return -1
}

func isActive(n Node) bool {
	switch n {
	case NodeDispatched, NodeInProgress, NodeInReview:
		return true
	}
	return false
}

// NodeFromMulticaStatus 把 Multica 父 issue 状态映射为大节点。
// 只认四个业务状态（字面、大小写敏感）；blocked / backlog / cancelled /
// 未知状态无映射——由调用方保持原节点（不推进）。
func NodeFromMulticaStatus(status string) (Node, bool) {
	switch status {
	case "todo":
		return NodeDispatched, true
	case "in_progress":
		return NodeInProgress, true
	case "in_review":
		return NodeInReview, true
	case "done":
		return NodeDelivered, true
	}
	return "", false
}

// CanTransition 报告 from→to 是否为合法跃迁。
//
// 冻结规则（契约，下游只读消费）：
//   - 主链只进不退，允许跨级（一个轮询窗口内 Multica 状态可能连跳，
//     禁跨级会卡死推进）；
//   - needs_decision 从任意活跃节点进入，可回到任意活跃节点或直达
//     delivered（决策后的去向由执行态决定）；
//   - intake 无 issue 无闸门，进不了 needs_decision；delivered 是终态。
func CanTransition(from, to Node) bool {
	if from == to {
		return false // 自跃迁不是跃迁
	}
	fi, ti := chainIndex(from), chainIndex(to)
	if fi >= 0 && ti >= 0 {
		return ti > fi // 主链：只进不退（含跨级）
	}
	if from == NodeNeedsDecision {
		return isActive(to) || to == NodeDelivered
	}
	if to == NodeNeedsDecision {
		return isActive(from)
	}
	return false
}

// Advance 用 Multica 父 issue 状态推进当前大节点：
// 不可映射（blocked/未知）、非法后退、目标等于当前——一律原样返回。
// infera 是单一状态源：Multica 侧的状态回退不导致节点回退。
func Advance(current Node, multicaStatus string) Node {
	mapped, ok := NodeFromMulticaStatus(multicaStatus)
	if !ok || mapped == current || !CanTransition(current, mapped) {
		return current
	}
	return mapped
}
