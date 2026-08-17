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
	SplitMode      bool      `json:"split_mode"`      // 父在规格审批选择了拆分
	MergeState     string    `json:"merge_state"`     // 父合并状态：'' | 'conflict'
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

// ChildSpec 拆分子需求规格（spec 审批时的拆分方案行；api/engine 共用，放 store 避免反向依赖）。
type ChildSpec struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Wave        int    `json:"wave"`
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
}
