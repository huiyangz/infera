package store

import (
	"context"
	"time"
)

type Project struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	RepoURL       string `json:"repo_url"`
	DefaultBranch string `json:"default_branch"`
	Pinned        bool   `json:"pinned"`
	// 外部来源映射（INFERA-79 T02 契约）：外部项目 ID 空 = 非同步来源；
	// synced_at nil = 从未同步。字段归同步入口（UpsertProjectByExternalID）所有。
	ExternalProjectID string     `json:"external_project_id"`
	ExternalSyncedAt  *time.Time `json:"external_synced_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type ProjectStats struct {
	Active  int       `json:"active"`
	Pending int       `json:"pending"`
	Last    time.Time `json:"last_activity"`
}

// RequirementStats 项目维度需求统计（INFERA-108 T01 冻结契约：
// GET /api/projects/{id}/stats 的响应载荷形态）。ByStatus 恒含五个固定键
// active/queued/completed/blocked/cancelled（无行时为 0，cancelled 为
// INFERA-232 新增的放弃终态）；Delivered 与 ByStatus["completed"] 同源
// （交付数单列是冻结口径，cancelled 不计入——放弃 ≠ 交付）。LastSyncedAt
// nil = 项目从未被 任务同步。
type RequirementStats struct {
	ProjectID        string         `json:"project_id"`
	RequirementTotal int            `json:"requirement_total"`
	ByStatus         map[string]int `json:"by_status"`
	PendingDecisions int            `json:"pending_decisions"`
	Delivered        int            `json:"delivered"`
	LastSyncedAt     *time.Time     `json:"last_synced_at"`
}

// PendingDecision 待人工决策的需求行（INFERA-108 T01 冻结契约：
// GET /api/pending-decisions 的响应载荷形态）。ID 即 delivery ID，
// 前端以其跳转既有需求详情；ProjectName 由查询 JOIN projects 带回。
type PendingDecision struct {
	ID               string    `json:"id"`
	ProjectID        string    `json:"project_id"`
	ProjectName      string    `json:"project_name"`
	Title            string    `json:"title"`
	Status           string    `json:"status"`
	PendingGate      string    `json:"pending_gate"`
	CurrentStage     string    `json:"current_stage"`
	ExternalIssueKey string    `json:"external_issue_key"` // ''=本地需求（非同步来源）
	Assignee         string    `json:"assignee"`           // 任务同步展示数据；''=无
	Priority         string    `json:"priority"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Delivery struct {
	ID             string `json:"id"`
	ProjectID      string `json:"project_id"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	Status         string `json:"status"` // active|queued|completed|blocked
	CurrentStage   string `json:"current_stage"`
	PendingGate    string `json:"pending_gate"`
	FailCount      int    `json:"fail_count"`
	BaseCommit     string `json:"base_commit"`
	RejectReason   string `json:"reject_reason"`   // 门禁驳回意见，重跑对应阶段时注入 prompt 后清空
	WorkspaceReady bool   `json:"workspace_ready"` // workspace 已就绪（幂等防重 clone/重建）
	ParentID       string `json:"parent_id"`       // 拆分子需求指向父 delivery（父/普通需求为空）
	Wave           int    `json:"wave"`            // 拆分批次号 1..N（父/普通需求=0）
	SplitMode      bool   `json:"split_mode"`      // 父在设计审批选择了拆分
	MergeState     string `json:"merge_state"`     // 父合并状态：'' | 'conflict'
	Complexity     string `json:"complexity"`      // 需求复杂度：''（老数据，按 small 走）| small | large（spec_approval 门裁定）
	// 外部来源映射（INFERA-79 T02 契约）：issue ID 空 = 非同步来源；synced_at nil = 从未同步。
	// issue_key 为展示键（如 INFERA-79）；assignee/priority 为同步进来的展示数据（非同步行为空）。
	// 这些字段归同步入口（UpsertDeliveryByExternalID）所有，UpdateDelivery 全行覆盖不冲掉。
	ExternalIssueID  string     `json:"external_issue_id"`
	ExternalIssueKey string     `json:"external_issue_key"`
	Assignee         string     `json:"assignee"`
	Priority         string     `json:"priority"`
	ExternalSyncedAt *time.Time `json:"external_synced_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type Event struct {
	ID         string    `json:"id"`
	DeliveryID string    `json:"delivery_id"`
	Stage      string    `json:"stage"`
	EventType  string    `json:"event_type"`
	Payload    []byte    `json:"payload"`
	CreatedAt  time.Time `json:"created_at"`
}

// Label workspace 级标签库行（INFERA-218 T01 冻结契约）：color 存上游 hex
// 原值（如 #22c55e），不做色彩换算。ExternalLabelID 是同步 upsert 的幂等键
// （上游标签 id；空 = 非同步来源/本地标签，不参与唯一性）。
type Label struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Color           string    `json:"color"`
	ExternalLabelID string    `json:"external_label_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
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
	// RequirementStats 项目维度需求统计（INFERA-108）：总数/按状态分布/
	// 待决策数/交付数/最近同步时间；项目不存在 → ErrNotFound。
	RequirementStats(ctx context.Context, id string) (RequirementStats, error)
	// ListPendingDecisions 跨项目取全部待人工决策需求（pending_gate 非空
	// 且未完结），JOIN projects 带 ProjectName，按 updated_at 降序。
	ListPendingDecisions(ctx context.Context) ([]PendingDecision, error)
	// 任务同步导入（T02 冻结的存储面）：按外部 ID 幂等 upsert——不存在则插入、
	// 存在则只更新外部来源字段（infera 侧配置/引擎字段不被同步覆盖），
	// 重复执行不产生重复行。外部 ID 为空 → ErrInvalid；delivery 引用不存在的
	// project → ErrNotFound。
	UpsertProjectByExternalID(ctx context.Context, p *Project) error
	// deliveries
	CreateDelivery(ctx context.Context, d *Delivery) error
	GetDelivery(ctx context.Context, id string) (*Delivery, error)
	ListProjectDeliveries(ctx context.Context, projectID string) ([]Delivery, error)
	ListActiveDeliveries(ctx context.Context) ([]Delivery, error)
	ListChildDeliveries(ctx context.Context, parentID string) ([]Delivery, error)
	// ListDeliveriesByLabelNames 跨项目按标签名取交付（需求发现视图，
	// INFERA-225 冻结契约）：挂有 names 中任一标签即命中（OR 语义），同一
	// 交付多标签命中只返回一次，按 updated_at 降序。names 空 = 无命中。
	ListDeliveriesByLabelNames(ctx context.Context, names []string) ([]Delivery, error)
	UpdateDelivery(ctx context.Context, d *Delivery) error
	UpsertDeliveryByExternalID(ctx context.Context, d *Delivery) error
	// labels（标签库，INFERA-218 T01 冻结）：CreateLabel 外部 ID 已被占用 →
	// ErrConflict；UpsertLabelByExternalID 按 上游标签 ID 幂等 upsert——不存在
	// 插入、存在只更新 name/color，重复执行不产生重复行；空外部 ID → ErrInvalid。
	CreateLabel(ctx context.Context, l *Label) error
	UpsertLabelByExternalID(ctx context.Context, l *Label) error
	ListLabels(ctx context.Context) ([]Label, error)
	// AttachLabel 给交付挂标签（幂等：重复挂同一标签不产生重复关联行）；
	// 交付或标签不存在 → ErrNotFound。DetachLabel 摘除关联，关联不存在 → ErrNotFound。
	AttachLabel(ctx context.Context, deliveryID, labelID string) error
	DetachLabel(ctx context.Context, deliveryID, labelID string) error
	// ListDeliveryLabels 单个交付挂的标签（完整 Label 行，按 name 升序，API 层
	// 投影为 name+color）；LabelsByDeliveryID 批量版（任务列表一次装配，免 N+1）。
	ListDeliveryLabels(ctx context.Context, deliveryID string) ([]Label, error)
	LabelsByDeliveryID(ctx context.Context, deliveryIDs []string) (map[string][]Label, error)
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
	// pipeline bindings（项目级专用——全局默认编排已删除）：空 projectID → ErrInvalid；
	// UpsertBinding 按 (project,node) 幂等覆盖。
	UpsertBinding(ctx context.Context, b *PipelineBinding) error
	DeleteBinding(ctx context.Context, projectID, node string) error
	ListBindings(ctx context.Context, projectID string) ([]PipelineBinding, error)
	// ListAllBindings 一次查询带回全部项目的绑定——全量扫描场景替代逐项目 N+1。
	ListAllBindings(ctx context.Context) ([]PipelineBinding, error)
	// ReplaceBindings 原子替换某项目的全部绑定（byNode: node→agentID；空=清空）：
	// 任一步失败整体回滚，不留半写。agent/项目不存在 → ErrNotFound。
	ReplaceBindings(ctx context.Context, projectID string, byNode map[string]string) error
}
