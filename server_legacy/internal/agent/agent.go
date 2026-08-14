package agent

import "context"

// Role 是专职 Agent 的角色，对应流水线上的某类工作。
type Role string

const (
	RoleSpec     Role = "spec"
	RoleTest     Role = "test"
	RoleCoder    Role = "coder"
	RoleReviewer Role = "reviewer"
)

// AgentConfig 是一个专职 Agent 的配置（来自 agent_configs 表）。
type AgentConfig struct {
	ID           string
	Name         string
	Role         Role
	SystemPrompt string // 注入到 Claude Code 的指令
	Model        string
}

// ExecInput 是一次执行的输入。
type ExecInput struct {
	Role    Role
	Prompt  string // 这次让 Agent 干什么（含上下文：需求/spec/代码等）
	Workdir string // 容器内工作目录（P2 可留空，P4 接真仓库后用）
}

// ExecResult 是一次执行的输出。
type ExecResult struct {
	SessionID string // 容器/执行 ID，便于追溯
	Output    string // Agent 产出文本（spec / 测试 / 代码 / 审核意见）
}

// Backend 抽象"在一个运行时里跑 Agent"。FakeBackend 测试，DockerBackend 生产。
type Backend interface {
	Execute(ctx context.Context, in ExecInput) (ExecResult, error)
}
