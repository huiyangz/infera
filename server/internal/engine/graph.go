// Package engine 驱动 delivery 按阶段图流转：
// agent 节点产出产物、人工门禁暂停等待、命令节点执行 intake/unit_test、终态释放 workspace。
package engine

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
	// RejectTo 驳回目标（门禁打回后回退重跑的阶段）。
	RejectTo string
	// ArtifactKind agent 产出的 artifact kind。
	ArtifactKind string
}

var Graph = map[string]Node{
	"intake":        {Stage: "intake", Kind: KindCommand, Next: "spec"},
	"spec":          {Stage: "spec", Kind: KindAgent, Next: "spec_approval", ArtifactKind: "spec"},
	"spec_approval": {Stage: "spec_approval", Kind: KindGate, Next: "test_gen", RejectTo: "spec"},
	"test_gen":      {Stage: "test_gen", Kind: KindAgent, Next: "code_gen", ArtifactKind: "tests"},
	"code_gen":      {Stage: "code_gen", Kind: KindAgent, Next: "unit_test", ArtifactKind: "diff"},
	"unit_test":     {Stage: "unit_test", Kind: KindCommand, Next: "code_review", OnFail: "code_gen"},
	"code_review":   {Stage: "code_review", Kind: KindGate, Next: "DONE", ReviewRole: "code_review", RejectTo: "code_gen"},
}

// MaxFail unit_test 连续失败上限，超过 = blocked。
const MaxFail = 3
