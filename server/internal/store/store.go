package store

import (
	"context"
	"time"
)

type Project struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	RepoURL       string    `json:"repo_url"`
	DefaultBranch string    `json:"default_branch"`
	Pinned        bool      `json:"pinned"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ProjectStats struct {
	Active  int       `json:"active"`
	Pending int       `json:"pending"`
	Last    time.Time `json:"last_activity"`
}

type Delivery struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	Title          string    `json:"title"`
	Description    string    `json:"description"`
	Status         string    `json:"status"` // active|queued|completed|blocked
	CurrentStage   string    `json:"current_stage"`
	PendingGate    string    `json:"pending_gate"`
	FailCount      int       `json:"fail_count"`
	BaseCommit     string    `json:"base_commit"`
	RejectReason   string    `json:"reject_reason"`   // 门禁驳回意见，重跑对应阶段时注入 prompt 后清空
	WorkspaceReady bool      `json:"workspace_ready"` // workspace 已就绪（幂等防重 clone/重建）
	ParentID       string    `json:"parent_id"`       // 拆分子需求指向父 delivery（父/普通需求为空）
	Wave           int       `json:"wave"`            // 拆分批次号 1..N（父/普通需求=0）
	SplitMode      bool      `json:"split_mode"`      // 父在设计审批选择了拆分
	MergeState     string    `json:"merge_state"`     // 父合并状态：'' | 'conflict'
	Complexity     string    `json:"complexity"`      // 需求复杂度：''（老数据，按 small 走）| small | large（spec_approval 门裁定）
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Event struct {
	ID         string    `json:"id"`
	DeliveryID string    `json:"delivery_id"`
	Stage      string    `json:"stage"`
	EventType  string    `json:"event_type"`
	Payload    []byte    `json:"payload"`
	CreatedAt  time.Time `json:"created_at"`
}

type Artifact struct {
	ID         string    `json:"id"`
	DeliveryID string    `json:"delivery_id"`
	Stage      string    `json:"stage"`
	Kind       string    `json:"kind"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

// ChildSpec 拆分子需求规格（设计审批时的拆分方案行；api/engine 共用，放 store 避免反向依赖）。
type ChildSpec struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Wave        int    `json:"wave"`
}

// TaskSpec 任务清单条目（tasks agent 产出 / tasks_approval 门可编辑覆盖；api/engine 共用）。
type TaskSpec struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// Finding 单条结构化审查意见（R10 双道审查契约，由本文件冻结）：
// 审查 agent 在输出末尾附 ```infera-findings fenced block（JSON 数组），
// 引擎容错解析（无块/坏 JSON → 空意见），报告 JSON 存 findings artifact。
type Finding struct {
	TaskIndex int    `json:"task_index"` // 关联任务序号（1-based；0=整体意见，不关联具体任务）
	Severity  string `json:"severity"`   // critical|major|minor|info（未知值归一为 info）
	Message   string `json:"message"`    // 意见内容（结论+理由）
	Evidence  string `json:"evidence"`   // 证据引用（file:line / 函数名 / 代码片段）
}

// FindingsReport 一道门禁前置审查的结构化产出（findings artifact 的 content 形状）。
type FindingsReport struct {
	Review    string    `json:"review"`     // spec_conformance|code_quality
	TaskBased bool      `json:"task_based"` // 规格符合性是否按任务清单逐项核验
	Findings  []Finding `json:"findings"`   // 结构化意见（空=无意见）
	Raw       string    `json:"raw"`        // agent 原始输出（畸形块时人工兜底阅读）
}

// findings artifact kind 约定（道名 + "_findings"）：引擎落盘与 API/前端读取共用。
const (
	KindSpecConformanceFindings = "spec_conformance_findings"
	KindCodeQualityFindings     = "code_quality_findings"
)

// ApproveOpts 门禁批准选项（api/engine 共用，放 store 避免反向依赖）。
// 单入口按当前门分发：spec_approval 用 Complexity；design_approval 用 Split；
// tasks_approval 用 Tasks。
type ApproveOpts struct {
	// Complexity 需求复杂度裁定（spec_approval 门）：small|large；
	// 空 = 取 spec 末尾 infera-complexity 块的建议，再无建议 = small。
	Complexity string `json:"complexity"`
	// Split 非空 = 「批准并拆分」（design_approval 门专用）。
	Split []ChildSpec `json:"split"`
	// Tasks 非空 = 批准时覆盖任务清单（tasks_approval 门专用，同 split 编辑器模式）。
	Tasks []TaskSpec `json:"tasks"`
}

// Agent 注册的执行者：runner 决定 config 语义（cli=command / http=url / docker=image+command / local=空）。
type Agent struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Runner    string         `json:"runner"` // cli|http|docker|local
	Config    map[string]any `json:"config"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// PipelineBinding 节点→Agent 绑定。ProjectID 空 = 全局默认，非空 = 项目覆盖。
type PipelineBinding struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Node      string    `json:"node"`
	AgentID   string    `json:"agent_id"`
	CreatedAt time.Time `json:"created_at"`
}

type StageRun struct {
	ID         string     `json:"id"`
	DeliveryID string     `json:"delivery_id"`
	Stage      string     `json:"stage"`
	Attempt    int        `json:"attempt"`
	Status     string     `json:"status"` // running|done|failed
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

type Store interface {
	// projects
	CreateProject(ctx context.Context, p *Project) error
	ListProjects(ctx context.Context) ([]Project, error)
	GetProject(ctx context.Context, id string) (*Project, error)
	PatchProjectPinned(ctx context.Context, id string, pinned bool) error
	ProjectStats(ctx context.Context, id string) (ProjectStats, error)
	// deliveries
	CreateDelivery(ctx context.Context, d *Delivery) error
	GetDelivery(ctx context.Context, id string) (*Delivery, error)
	ListProjectDeliveries(ctx context.Context, projectID string) ([]Delivery, error)
	ListActiveDeliveries(ctx context.Context) ([]Delivery, error)
	ListChildDeliveries(ctx context.Context, parentID string) ([]Delivery, error)
	UpdateDelivery(ctx context.Context, d *Delivery) error
	// events / artifacts / stage_runs
	AppendEvent(ctx context.Context, e *Event) error
	ListEvents(ctx context.Context, deliveryID string) ([]Event, error)
	SaveArtifact(ctx context.Context, a *Artifact) error
	ListArtifacts(ctx context.Context, deliveryID string) ([]Artifact, error)
	LatestArtifact(ctx context.Context, deliveryID, kind string) (*Artifact, error)
	StartStageRun(ctx context.Context, r *StageRun) error
	FinishStageRun(ctx context.Context, id string, status string) error
	LatestStageRun(ctx context.Context, deliveryID, stage string) (*StageRun, error)
	// agents（注册表）：name 唯一冲突 → ErrConflict；删除仍被绑定引用的 agent → ErrConflict。
	CreateAgent(ctx context.Context, a *Agent) error
	ListAgents(ctx context.Context) ([]Agent, error)
	GetAgent(ctx context.Context, id string) (*Agent, error)
	UpdateAgent(ctx context.Context, a *Agent) error
	DeleteAgent(ctx context.Context, id string) error
	// pipeline bindings：projectID 空 = 全局默认；UpsertBinding 按 (project,node) 幂等覆盖。
	UpsertBinding(ctx context.Context, b *PipelineBinding) error
	DeleteBinding(ctx context.Context, projectID, node string) error
	ListBindings(ctx context.Context, projectID string) ([]PipelineBinding, error)
	// ListAllBindings 一次查询带回全部绑定（全局默认 + 所有项目覆盖；
	// 全局默认行 ProjectID 为空串）——全量扫描场景替代逐项目 N+1。
	ListAllBindings(ctx context.Context) ([]PipelineBinding, error)
	// ReplaceBindings 原子替换某项目的全部绑定（byNode: node→agentID；空=清空）：
	// 任一步失败整体回滚，不留半写。agent/项目不存在 → ErrNotFound。
	ReplaceBindings(ctx context.Context, projectID string, byNode map[string]string) error
}
