// Package engine 驱动 delivery 按阶段图流转：
// agent 节点产出产物、人工门禁暂停等待、命令节点执行 intake/unit_test、终态释放 workspace。
package engine

// Kind 阶段图：引擎只认识节点类型与下一跳。
type Kind int

const (
	KindAgent    Kind = iota // 需要 workdir 的 agent 节点
	KindGate                 // 人工门禁（暂停）
	KindCommand              // 命令节点（intake=Acquire 标志 / unit_test=跑测试）
	KindTerminal             // 预留：当前图无此类节点，终态统一由 advance 的 DONE 处理
)

// Node 图节点：成功下一跳 + 失败下一跳（回环）。
type Node struct {
	Stage  string
	Kind   Kind
	Next   string // 成功下一跳
	OnFail string // 失败下一跳（unit_test 回环用）
}

// Stages 全计划唯一阶段清单（顺序即展示顺序）。
var Stages = []string{"intake", "spec", "spec_approval", "test_gen", "code_gen", "unit_test", "code_review"}

var Graph = map[string]Node{
	"intake":        {Stage: "intake", Kind: KindCommand, Next: "spec"},
	"spec":          {Stage: "spec", Kind: KindAgent, Next: "spec_approval"},
	"spec_approval": {Stage: "spec_approval", Kind: KindGate, Next: "test_gen"},
	"test_gen":      {Stage: "test_gen", Kind: KindAgent, Next: "code_gen"},
	"code_gen":      {Stage: "code_gen", Kind: KindAgent, Next: "unit_test"},
	"unit_test":     {Stage: "unit_test", Kind: KindCommand, Next: "code_review", OnFail: "code_gen"},
	"code_review":   {Stage: "code_review", Kind: KindGate, Next: "DONE"},
}

// MaxFail unit_test 连续失败上限，超过 = blocked。
const MaxFail = 3
