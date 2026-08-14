package agent

// stageToRole 把需要 Agent 执行的 stage 映射到专职 role。
// 返回 ok=false 表示该 stage 由人或系统处理，不调 Agent。
var stageToRole = map[string]Role{
	"spec":        RoleSpec,
	"test_gen":    RoleTest,
	"code_gen":    RoleCoder,
	"code_review": RoleReviewer,
}

// RoleForStage 返回 stage 对应的专职 role；若该 stage 不需要 Agent（人/系统），ok=false。
func RoleForStage(stage string) (Role, bool) {
	r, ok := stageToRole[stage]
	return r, ok
}
