package stage

// 固定的 stage 序列（P1 静态模板）。顺序即推进顺序。
var order = []string{
	"intake",        // 需求
	"spec",          // Spec Agent 写 spec
	"spec_approval", // 人审批 spec（gate）
	"test_gen",      // Test Agent 生成用例 + 单测
	"code_gen",      // Coder Agent 写实现（修复 hub）
	"unit_test",     // 系统跑单测
	"code_review",   // Reviewer Agent 预审 + 人批准（gate）
	"deploy",        // 部署
}

var gates = map[string]bool{
	"spec_approval": true,
	"code_review":   true,
}

// All 返回全部 stage 的拷贝。
func All() []string {
	out := make([]string, len(order))
	copy(out, order)
	return out
}

// Next 返回 from 的下一个 stage；若 from 是最后一个，ok=false。
func Next(from string) (string, bool) {
	for i, s := range order {
		if s == from && i+1 < len(order) {
			return order[i+1], true
		}
	}
	return "", false
}

// IsGate 报告 s 是否是需要人介入的 gate stage。
func IsGate(s string) bool {
	return gates[s]
}

// IsValid 报告 s 是否是合法 stage。
func IsValid(s string) bool {
	for _, x := range order {
		if x == s {
			return true
		}
	}
	return false
}
