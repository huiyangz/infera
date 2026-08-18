// Package engine 驱动 delivery 按阶段图流转：
// agent 节点产出产物、人工门禁暂停等待、命令节点执行 intake/unit_test、终态释放 workspace。
package engine

import "github.com/tokfinity/infera/internal/store"

// Kind 阶段图：引擎只认识节点类型与下一跳。
type Kind int

const (
	KindAgent   Kind = iota // 需要 workdir 的 agent 节点
	KindGate                // 人工门禁（暂停）
	KindCommand             // 命令节点（intake=Acquire 标志 / unit_test=跑测试）
)

// Node 图节点：成功下一跳 + 失败下一跳（回环）+ 阶段元数据（单一事实来源）。
type Node struct {
	Stage  string
	Kind   Kind
	Next   string // 成功下一跳
	OnFail string // 失败下一跳（unit_test 回环用）
	// ReviewRole 门禁前置 agent：挂门禁前先跑（code_review 需要审查员预审产出 artifact）。
	ReviewRole string
	// FindingsReviews 门禁前置的结构化审查道（R10，code_review）：每道独立绑定 agent、
	// 产出 findings artifact；全部产出后门禁才挂起（缺绑定/失败 → blocked，见 reviews.go）。
	FindingsReviews []string
	// RejectTo 驳回目标（门禁打回后回退重跑的阶段）。
	RejectTo string
	// ArtifactKind agent 产出的 artifact kind。
	ArtifactKind string
	// Persist 挂门禁前先固化产出（commit/push/PR + 真 diff artifact）。
	Persist bool
}

// 复杂度取值（spec_approval 门裁定；” 为老数据，按 small 走）。
const (
	ComplexitySmall = "small"
	ComplexityLarge = "large"
)

// Graph 11 阶段图（阶段枚举与序的单一事实来源，StageOrder 为展示全序）：
//
//	intake → spec → spec_approval ─[small/'' ]→ test_gen → code_gen → unit_test → code_review → DONE
//	                          └[large]→ design → design_approval ─┬[拆分]→ 父停 code_gen（合并，机制同现状）
//	                                                          └──────→ tasks → tasks_approval → test_gen → …
//
// spec_approval 的实际下一跳按复杂度分岔（nextAfterGate）；
// design_approval 的分岔在 approveSplit（批准并拆分）处理。
var Graph = map[string]Node{
	"intake":          {Stage: "intake", Kind: KindCommand, Next: "spec"},
	"spec":            {Stage: "spec", Kind: KindAgent, Next: "spec_approval", ArtifactKind: "spec"},
	"spec_approval":   {Stage: "spec_approval", Kind: KindGate, Next: "test_gen", RejectTo: "spec"},
	"design":          {Stage: "design", Kind: KindAgent, Next: "design_approval", ArtifactKind: "design"},
	"design_approval": {Stage: "design_approval", Kind: KindGate, Next: "tasks", RejectTo: "design"},
	"tasks":           {Stage: "tasks", Kind: KindAgent, Next: "tasks_approval", ArtifactKind: "tasks"},
	"tasks_approval":  {Stage: "tasks_approval", Kind: KindGate, Next: "test_gen", RejectTo: "tasks"},
	"test_gen":        {Stage: "test_gen", Kind: KindAgent, Next: "code_gen", ArtifactKind: "tests"},
	// code_gen 的改动摘要 kind=summary；真正的 diff 由 Persist 在 code_review 门禁产出。
	"code_gen":    {Stage: "code_gen", Kind: KindAgent, Next: "unit_test", ArtifactKind: "summary"},
	"unit_test":   {Stage: "unit_test", Kind: KindCommand, Next: "code_review", OnFail: "code_gen"},
	"code_review": {Stage: "code_review", Kind: KindGate, Next: "DONE", ReviewRole: "code_review", RejectTo: "code_gen", Persist: true, FindingsReviews: []string{"spec_conformance", "code_quality"}},
}

// StageOrder 阶段全序（11 阶段；前端阶段条与外部消费方的展示基准。
// small 直跳 test_gen、拆分父跳过 tasks/tasks_approval/test_gen 属展示层按交付模式派生）。
var StageOrder = []string{
	"intake", "spec", "spec_approval",
	"design", "design_approval", "tasks", "tasks_approval",
	"test_gen", "code_gen", "unit_test", "code_review",
}

// nextAfterGate 门禁放行后的下一跳：spec_approval 按复杂度分岔
// （large→design；small 与老数据 ”→test_gen），其余门走静态 Next。
// design_approval 的拆分分岔（父停 code_gen）不经此函数，见 approveSplit。
func nextAfterGate(d *store.Delivery, node Node) string {
	if node.Stage == "spec_approval" && d.Complexity == ComplexityLarge {
		return "design"
	}
	return node.Next
}

// MaxFail unit_test 连续失败上限，超过 = blocked。
const MaxFail = 3
