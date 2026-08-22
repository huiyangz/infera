package store

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pg 是基于 pgx 连接池的 Store 实现，语义与 Memory 保持一致。
type Pg struct {
	pool *pgxpool.Pool
}

func NewPg(pool *pgxpool.Pool) *Pg { return &Pg{pool: pool} }

var _ Store = (*Pg)(nil)

// mapErr 把 pgx.ErrNoRows 映射为 ErrNotFound。
func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// isUnique / isFKViolation 识别 PG 约束冲突（23505 唯一 / 23503 外键）。
func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isFKViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

const (
	projectCols  = "id,name,repo_url,default_branch,pinned,external_project_id,external_synced_at,created_at,updated_at"
	deliveryCols = "id,project_id,title,description,status,current_stage,pending_gate,fail_count,base_commit,reject_reason,workspace_ready,parent_id,wave,split_mode,merge_state,complexity,external_issue_id,external_issue_key,assignee,priority,external_synced_at,created_at,updated_at"
	stageRunCols = "id,delivery_id,stage,attempt,status,started_at,finished_at"
)

// scan helpers：单行查询与列表路径共用同一份 scan 目标列表（pgx.Rows 满足 pgx.Row）。

func scanProject(row pgx.Row) (*Project, error) {
	p := &Project{}
	if err := row.Scan(&p.ID, &p.Name, &p.RepoURL, &p.DefaultBranch, &p.Pinned, &p.ExternalProjectID, &p.ExternalSyncedAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	return p, nil
}

func scanDelivery(row pgx.Row) (*Delivery, error) {
	d := &Delivery{}
	var parentID sql.NullString
	if err := row.Scan(&d.ID, &d.ProjectID, &d.Title, &d.Description, &d.Status, &d.CurrentStage, &d.PendingGate, &d.FailCount, &d.BaseCommit, &d.RejectReason, &d.WorkspaceReady, &parentID, &d.Wave, &d.SplitMode, &d.MergeState, &d.Complexity, &d.ExternalIssueID, &d.ExternalIssueKey, &d.Assignee, &d.Priority, &d.ExternalSyncedAt, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	d.ParentID = parentID.String
	return d, nil
}

// nullableParent 把空串映射为 SQL NULL（parent_id 列可空）。
func nullableParent(id string) any {
	if id == "" {
		return nil
	}
	return id
}

func scanStageRun(row pgx.Row) (*StageRun, error) {
	r := &StageRun{}
	if err := row.Scan(&r.ID, &r.DeliveryID, &r.Stage, &r.Attempt, &r.Status, &r.StartedAt, &r.FinishedAt); err != nil {
		return nil, mapErr(err)
	}
	return r, nil
}

// projects

func (pg *Pg) CreateProject(ctx context.Context, p *Project) error {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	// 时间戳交给 DB 默认值，插入后回读整行填充结构体。
	_, err := pg.pool.Exec(ctx,
		`INSERT INTO projects (id,name,repo_url,default_branch,pinned) VALUES ($1,$2,$3,$4,$5)`,
		p.ID, p.Name, p.RepoURL, p.DefaultBranch, p.Pinned)
	if err != nil {
		return err
	}
	got, err := pg.GetProject(ctx, p.ID)
	if err != nil {
		return err
	}
	*p = *got
	return nil
}

func (pg *Pg) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := pg.pool.Query(ctx, `SELECT `+projectCols+` FROM projects ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Project, 0)
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (pg *Pg) GetProject(ctx context.Context, id string) (*Project, error) {
	return scanProject(pg.pool.QueryRow(ctx, `SELECT `+projectCols+` FROM projects WHERE id=$1`, id))
}

func (pg *Pg) PatchProjectPinned(ctx context.Context, id string, pinned bool) error {
	tag, err := pg.pool.Exec(ctx, `UPDATE projects SET pinned=$2, updated_at=now() WHERE id=$1`, id, pinned)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ProjectStats 单条 SQL 聚合：活跃数、门禁数、最近活动时间与项目存在性一次带回。
// Last = max(project.updated_at, max(delivery.updated_at))（greatest 忽略 NULL，
// 无 delivery 时退化为 project.updated_at，与 memory 实现语义一致）。
func (pg *Pg) ProjectStats(ctx context.Context, id string) (ProjectStats, error) {
	var (
		active, pending int
		last            sql.NullTime // 项目不存在时两参数皆 NULL
		exists          bool
	)
	err := pg.pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE status='active'),
		        count(*) FILTER (WHERE pending_gate<>''),
		        greatest(max(updated_at), (SELECT updated_at FROM projects WHERE id=$1)),
		        EXISTS(SELECT 1 FROM projects WHERE id=$1)
		 FROM deliveries WHERE project_id=$1`, id).
		Scan(&active, &pending, &last, &exists)
	if err != nil {
		return ProjectStats{}, err
	}
	if !exists {
		return ProjectStats{}, ErrNotFound
	}
	return ProjectStats{Active: active, Pending: pending, Last: last.Time}, nil
}

// RequirementStats 单条 SQL 聚合（同 ProjectStats 形态）：总数与各状态桶、
// 待决策数（pending_gate 非空且未完结）、交付数与最近同步时间一次带回，
// EXISTS 兜底项目存在性（无 delivery 的项目聚合行全零，仍需区分 404）。
func (pg *Pg) RequirementStats(ctx context.Context, id string) (RequirementStats, error) {
	var (
		total, active, queued, completed, blocked, pending int
		synced                                             sql.NullTime
		exists                                             bool
	)
	err := pg.pool.QueryRow(ctx,
		`SELECT count(*),
		        count(*) FILTER (WHERE status='active'),
		        count(*) FILTER (WHERE status='queued'),
		        count(*) FILTER (WHERE status='completed'),
		        count(*) FILTER (WHERE status='blocked'),
		        count(*) FILTER (WHERE pending_gate<>'' AND status<>'completed'),
		        (SELECT external_synced_at FROM projects WHERE id=$1),
		        EXISTS(SELECT 1 FROM projects WHERE id=$1)
		 FROM deliveries WHERE project_id=$1`, id).
		Scan(&total, &active, &queued, &completed, &blocked, &pending, &synced, &exists)
	if err != nil {
		return RequirementStats{}, err
	}
	if !exists {
		return RequirementStats{}, ErrNotFound
	}
	s := RequirementStats{
		ProjectID:        id,
		RequirementTotal: total,
		ByStatus:         map[string]int{"active": active, "queued": queued, "completed": completed, "blocked": blocked},
		PendingDecisions: pending,
		Delivered:        completed,
	}
	if synced.Valid {
		t := synced.Time
		s.LastSyncedAt = &t
	}
	return s, nil
}

// ListPendingDecisions 跨项目取全部待人工决策需求（pending_gate 非空且未
// 完结），JOIN projects 带 ProjectName，按 updated_at 降序。
func (pg *Pg) ListPendingDecisions(ctx context.Context) ([]PendingDecision, error) {
	rows, err := pg.pool.Query(ctx,
		`SELECT d.id, d.project_id, p.name, d.title, d.status, d.pending_gate, d.current_stage,
		        d.external_issue_key, d.assignee, d.priority, d.created_at, d.updated_at
		 FROM deliveries d JOIN projects p ON p.id = d.project_id
		 WHERE d.pending_gate <> '' AND d.status <> 'completed'
		 ORDER BY d.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PendingDecision, 0)
	for rows.Next() {
		var r PendingDecision
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.ProjectName, &r.Title, &r.Status, &r.PendingGate,
			&r.CurrentStage, &r.ExternalIssueKey, &r.Assignee, &r.Priority, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpsertProjectByExternalID 按 上游项目 ID 幂等导入（同步链路唯一入口，语义与 Memory 一致）：
// ON CONFLICT 命中部分唯一索引（空串不参与唯一性）→ 更新 name、synced_at 与
// repo_url（覆写契约 INFERA-175：EXCLUDED 非空覆写现值，空串保留现值不清空，
// COALESCE+NULLIF 一处收口）；default_branch/pinned 归 infera 侧配置，冲突分支不覆盖。
// RETURNING id 两个分支都回行 ID，回读整行填充结构体。
// ExternalProjectID 为空 → ErrInvalid。
func (pg *Pg) UpsertProjectByExternalID(ctx context.Context, p *Project) error {
	if p.ExternalProjectID == "" {
		return ErrInvalid
	}
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	var id string
	err := pg.pool.QueryRow(ctx,
		`INSERT INTO projects (id,name,repo_url,default_branch,pinned,external_project_id,external_synced_at)
		 VALUES ($1,$2,$3,$4,$5,$6,now())
		 ON CONFLICT (external_project_id) WHERE external_project_id <> ''
		 DO UPDATE SET name=EXCLUDED.name,
		               repo_url=COALESCE(NULLIF(EXCLUDED.repo_url, ''), projects.repo_url),
		               external_synced_at=now()
		 RETURNING id`,
		p.ID, p.Name, p.RepoURL, p.DefaultBranch, p.Pinned, p.ExternalProjectID).Scan(&id)
	if err != nil {
		return err
	}
	got, err := pg.GetProject(ctx, id)
	if err != nil {
		return err
	}
	*p = *got
	return nil
}

// deliveries

func (pg *Pg) CreateDelivery(ctx context.Context, d *Delivery) error {
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	_, err := pg.pool.Exec(ctx,
		`INSERT INTO deliveries (id,project_id,title,description,status,current_stage,pending_gate,fail_count,base_commit,reject_reason,workspace_ready,parent_id,wave,split_mode,merge_state,complexity)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		d.ID, d.ProjectID, d.Title, d.Description, d.Status, d.CurrentStage, d.PendingGate, d.FailCount, d.BaseCommit, d.RejectReason, d.WorkspaceReady, nullableParent(d.ParentID), d.Wave, d.SplitMode, d.MergeState, d.Complexity)
	if err != nil {
		return err
	}
	got, err := pg.GetDelivery(ctx, d.ID)
	if err != nil {
		return err
	}
	*d = *got
	return nil
}

func (pg *Pg) GetDelivery(ctx context.Context, id string) (*Delivery, error) {
	return scanDelivery(pg.pool.QueryRow(ctx, `SELECT `+deliveryCols+` FROM deliveries WHERE id=$1`, id))
}

func (pg *Pg) ListProjectDeliveries(ctx context.Context, projectID string) ([]Delivery, error) {
	rows, err := pg.pool.Query(ctx, `SELECT `+deliveryCols+` FROM deliveries WHERE project_id=$1 ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Delivery, 0)
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// ListActiveDeliveries 跨项目取所有 active 交付（重启恢复用），按创建时间升序。
func (pg *Pg) ListActiveDeliveries(ctx context.Context) ([]Delivery, error) {
	rows, err := pg.pool.Query(ctx, `SELECT `+deliveryCols+` FROM deliveries WHERE status='active' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Delivery, 0)
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// UpdateDelivery 按读到的 updated_at 条件更新（乐观锁，同 UpdateAgent）：
// 并发读-改-写的后写者条件不命中 → 回读定性（不存在 → ErrNotFound；版本过期 → ErrConflict），
// 全行覆盖不得静默冲掉并发修改。SET 不含外部来源列——同步映射字段归
// UpsertDeliveryByExternalID 所有，普通更新路径不冲掉（同 Memory 的字段保留语义）。
func (pg *Pg) UpdateDelivery(ctx context.Context, d *Delivery) error {
	err := pg.pool.QueryRow(ctx,
		`UPDATE deliveries SET title=$2,description=$3,status=$4,current_stage=$5,pending_gate=$6,fail_count=$7,base_commit=$8,reject_reason=$9,workspace_ready=$10,parent_id=$11,wave=$12,split_mode=$13,merge_state=$14,complexity=$15,updated_at=now()
		 WHERE id=$1 AND updated_at=$16 RETURNING updated_at`,
		d.ID, d.Title, d.Description, d.Status, d.CurrentStage, d.PendingGate, d.FailCount, d.BaseCommit, d.RejectReason, d.WorkspaceReady, nullableParent(d.ParentID), d.Wave, d.SplitMode, d.MergeState, d.Complexity, d.UpdatedAt).
		Scan(&d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, gerr := pg.GetDelivery(ctx, d.ID); gerr != nil {
			return gerr // ErrNotFound 或底层错误
		}
		return ErrConflict // 行在、版本过期：并发覆盖
	}
	return mapErr(err)
}

// UpsertDeliveryByExternalID 按 上游 issue ID 幂等导入（同步链路唯一入口，语义与 Memory 一致）：
// ON CONFLICT 命中部分唯一索引（空串不参与唯一性）→ 只更新外部来源字段
// （project_id/title/description/status/parent_id/wave/issue_key/assignee/priority），
// 引擎侧字段（stage/gate/fail_count/...）不被同步覆盖；插入分支整行走入参（同 CreateDelivery）。
// RETURNING id 两个分支都回行 ID，回读整行填充结构体。
// ExternalIssueID 为空 → ErrInvalid；ProjectID 不存在（FK 23503）→ ErrNotFound。
func (pg *Pg) UpsertDeliveryByExternalID(ctx context.Context, d *Delivery) error {
	if d.ExternalIssueID == "" {
		return ErrInvalid
	}
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	var id string
	err := pg.pool.QueryRow(ctx,
		`INSERT INTO deliveries (id,project_id,title,description,status,current_stage,pending_gate,fail_count,base_commit,reject_reason,workspace_ready,parent_id,wave,split_mode,merge_state,complexity,external_issue_id,external_issue_key,assignee,priority,external_synced_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,now())
		 ON CONFLICT (external_issue_id) WHERE external_issue_id <> ''
		 DO UPDATE SET project_id=EXCLUDED.project_id,
		               title=EXCLUDED.title,
		               description=EXCLUDED.description,
		               status=EXCLUDED.status,
		               parent_id=EXCLUDED.parent_id,
		               wave=EXCLUDED.wave,
		               external_issue_key=EXCLUDED.external_issue_key,
		               assignee=EXCLUDED.assignee,
		               priority=EXCLUDED.priority,
		               external_synced_at=now()
		 RETURNING id`,
		d.ID, d.ProjectID, d.Title, d.Description, d.Status, d.CurrentStage, d.PendingGate, d.FailCount, d.BaseCommit, d.RejectReason, d.WorkspaceReady, nullableParent(d.ParentID), d.Wave, d.SplitMode, d.MergeState, d.Complexity, d.ExternalIssueID, d.ExternalIssueKey, d.Assignee, d.Priority).Scan(&id)
	if isFKViolation(err) { // project_id 不存在
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	got, err := pg.GetDelivery(ctx, id)
	if err != nil {
		return err
	}
	*d = *got
	return nil
}

// ListChildDeliveries 取某父 delivery 的全部子需求，按批次号、创建时间升序。
func (pg *Pg) ListChildDeliveries(ctx context.Context, parentID string) ([]Delivery, error) {
	rows, err := pg.pool.Query(ctx, `SELECT `+deliveryCols+` FROM deliveries WHERE parent_id=$1 ORDER BY wave, created_at`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Delivery, 0)
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// events / artifacts / stage_runs

// AppendEvent 单语句插入并 RETURNING created_at：插入与回读原子，
// 不留"行已插入但调用方拿不到时间戳"的中间态。
func (pg *Pg) AppendEvent(ctx context.Context, e *Event) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	payload := e.Payload
	if payload == nil {
		payload = []byte(`{}`) // payload 列 NOT NULL
	}
	return mapErr(pg.pool.QueryRow(ctx,
		`INSERT INTO events (id,delivery_id,stage,event_type,payload) VALUES ($1,$2,$3,$4,$5) RETURNING created_at`,
		e.ID, e.DeliveryID, e.Stage, e.EventType, payload).Scan(&e.CreatedAt))
}

func (pg *Pg) ListEvents(ctx context.Context, deliveryID string) ([]Event, error) {
	rows, err := pg.pool.Query(ctx,
		`SELECT id,delivery_id,stage,event_type,payload,created_at FROM events WHERE delivery_id=$1 ORDER BY created_at`, deliveryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Event, 0)
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.DeliveryID, &e.Stage, &e.EventType, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SaveArtifact 单语句插入并 RETURNING created_at（与 AppendEvent 同理，原子回读）。
func (pg *Pg) SaveArtifact(ctx context.Context, a *Artifact) error {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	return mapErr(pg.pool.QueryRow(ctx,
		`INSERT INTO artifacts (id,delivery_id,stage,kind,content) VALUES ($1,$2,$3,$4,$5) RETURNING created_at`,
		a.ID, a.DeliveryID, a.Stage, a.Kind, a.Content).Scan(&a.CreatedAt))
}

// LatestArtifact 取指定 kind 的最新一条产物（无则 ErrNotFound）。
func (pg *Pg) LatestArtifact(ctx context.Context, deliveryID, kind string) (*Artifact, error) {
	var a Artifact
	err := pg.pool.QueryRow(ctx,
		`SELECT id,delivery_id,stage,kind,content,created_at FROM artifacts WHERE delivery_id=$1 AND kind=$2 ORDER BY created_at DESC LIMIT 1`,
		deliveryID, kind).
		Scan(&a.ID, &a.DeliveryID, &a.Stage, &a.Kind, &a.Content, &a.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &a, nil
}

func (pg *Pg) ListArtifacts(ctx context.Context, deliveryID string) ([]Artifact, error) {
	rows, err := pg.pool.Query(ctx,
		`SELECT id,delivery_id,stage,kind,content,created_at FROM artifacts WHERE delivery_id=$1 ORDER BY created_at`, deliveryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Artifact, 0)
	for rows.Next() {
		var a Artifact
		if err := rows.Scan(&a.ID, &a.DeliveryID, &a.Stage, &a.Kind, &a.Content, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (pg *Pg) StartStageRun(ctx context.Context, r *StageRun) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	// status/started_at 交给 DB 默认值（'running'/now()）。
	_, err := pg.pool.Exec(ctx,
		`INSERT INTO stage_runs (id,delivery_id,stage,attempt) VALUES ($1,$2,$3,$4)`,
		r.ID, r.DeliveryID, r.Stage, r.Attempt)
	if err != nil {
		return err
	}
	got, err := scanStageRun(pg.pool.QueryRow(ctx, `SELECT `+stageRunCols+` FROM stage_runs WHERE id=$1`, r.ID))
	if err != nil {
		return err
	}
	*r = *got
	return nil
}

func (pg *Pg) FinishStageRun(ctx context.Context, id string, status string) error {
	tag, err := pg.pool.Exec(ctx, `UPDATE stage_runs SET status=$2, finished_at=now() WHERE id=$1`, id, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// LatestStageRun 取该阶段最近一次运行。started_at 并列时按 attempt、id 稳定排序
// （attempt 由调用方按 latest 递增分配，天然单调），防同刻并列取错旧行。
func (pg *Pg) LatestStageRun(ctx context.Context, deliveryID, stage string) (*StageRun, error) {
	return scanStageRun(pg.pool.QueryRow(ctx,
		`SELECT `+stageRunCols+` FROM stage_runs WHERE delivery_id=$1 AND stage=$2 ORDER BY started_at DESC, attempt DESC, id DESC LIMIT 1`,
		deliveryID, stage))
}

// agents / pipeline bindings

const agentCols = "id,name,runner,config,created_at,updated_at"

func scanAgent(row pgx.Row) (*Agent, error) {
	a := &Agent{}
	if err := row.Scan(&a.ID, &a.Name, &a.Runner, &a.Config, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	if a.Config == nil {
		a.Config = map[string]any{}
	}
	return a, nil
}

func (pg *Pg) CreateAgent(ctx context.Context, a *Agent) error {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	cfg := a.Config
	if cfg == nil {
		cfg = map[string]any{}
	}
	_, err := pg.pool.Exec(ctx,
		`INSERT INTO agents (id,name,runner,config) VALUES ($1,$2,$3,$4)`,
		a.ID, a.Name, a.Runner, cfg)
	if isUnique(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	got, err := pg.GetAgent(ctx, a.ID)
	if err != nil {
		return err
	}
	*a = *got
	return nil
}

func (pg *Pg) ListAgents(ctx context.Context) ([]Agent, error) {
	rows, err := pg.pool.Query(ctx, `SELECT `+agentCols+` FROM agents ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Agent, 0)
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (pg *Pg) GetAgent(ctx context.Context, id string) (*Agent, error) {
	return scanAgent(pg.pool.QueryRow(ctx, `SELECT `+agentCols+` FROM agents WHERE id=$1`, id))
}

// UpdateAgent 按读到的 updated_at 条件更新（乐观锁）：并发读-改-写的后写者
// 条件不命中 → 回读定性（不存在 → ErrNotFound；版本过期 → ErrConflict）。
func (pg *Pg) UpdateAgent(ctx context.Context, a *Agent) error {
	cfg := a.Config
	if cfg == nil {
		cfg = map[string]any{}
	}
	err := pg.pool.QueryRow(ctx,
		`UPDATE agents SET name=$2,runner=$3,config=$4,updated_at=now()
		 WHERE id=$1 AND updated_at=$5 RETURNING updated_at`,
		a.ID, a.Name, a.Runner, cfg, a.UpdatedAt).Scan(&a.UpdatedAt)
	if isUnique(err) {
		return ErrConflict
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if _, gerr := pg.GetAgent(ctx, a.ID); gerr != nil {
			return gerr // ErrNotFound 或底层错误
		}
		return ErrConflict // 行在、版本过期：并发覆盖
	}
	return mapErr(err)
}

func (pg *Pg) DeleteAgent(ctx context.Context, id string) error {
	tag, err := pg.pool.Exec(ctx, `DELETE FROM agents WHERE id=$1`, id)
	if err != nil {
		if isFKViolation(err) { // 仍被 pipeline_bindings 引用（理论上行级锁竞态下出现）
			return ErrConflict
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpsertBinding 按 (project,node) 幂等覆盖。project_id 空串 = 全局默认。
// 部分唯一索引的 NULL 语义使两条 ON CONFLICT 分支必须分开写。
func (pg *Pg) UpsertBinding(ctx context.Context, b *PipelineBinding) error {
	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	var err error
	if b.ProjectID == "" {
		_, err = pg.pool.Exec(ctx,
			`INSERT INTO pipeline_bindings (id,project_id,node,agent_id) VALUES ($1,NULL,$2,$3)
			 ON CONFLICT (node) WHERE project_id IS NULL DO UPDATE SET agent_id=EXCLUDED.agent_id`,
			b.ID, b.Node, b.AgentID)
	} else {
		_, err = pg.pool.Exec(ctx,
			`INSERT INTO pipeline_bindings (id,project_id,node,agent_id) VALUES ($1,$2,$3,$4)
			 ON CONFLICT (project_id,node) WHERE project_id IS NOT NULL DO UPDATE SET agent_id=EXCLUDED.agent_id`,
			b.ID, b.ProjectID, b.Node, b.AgentID)
	}
	if isFKViolation(err) { // agent_id / project_id 不存在
		return ErrNotFound
	}
	return err
}

func (pg *Pg) DeleteBinding(ctx context.Context, projectID, node string) error {
	var tag pgconn.CommandTag
	var err error
	if projectID == "" {
		tag, err = pg.pool.Exec(ctx, `DELETE FROM pipeline_bindings WHERE project_id IS NULL AND node=$1`, node)
	} else {
		tag, err = pg.pool.Exec(ctx, `DELETE FROM pipeline_bindings WHERE project_id=$1 AND node=$2`, projectID, node)
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ReplaceBindings 单事务原子替换某项目的全部绑定：先删旧、再按节点序写入新集合，
// 任一步失败整体回滚（调用方拿到错误时原状态一字不差）。projectID 空串 = 全局默认。
// 按节点序插入使失败位置确定：排在失败项之前的写入已真实落过，回滚覆盖的正是半写场景。
func (pg *Pg) ReplaceBindings(ctx context.Context, projectID string, byNode map[string]string) error {
	err := pgx.BeginFunc(ctx, pg.pool, func(tx pgx.Tx) error {
		var err error
		if projectID == "" {
			_, err = tx.Exec(ctx, `DELETE FROM pipeline_bindings WHERE project_id IS NULL`)
		} else {
			_, err = tx.Exec(ctx, `DELETE FROM pipeline_bindings WHERE project_id=$1`, projectID)
		}
		if err != nil {
			return err
		}
		for _, node := range slices.Sorted(maps.Keys(byNode)) {
			if _, err := tx.Exec(ctx,
				`INSERT INTO pipeline_bindings (id,project_id,node,agent_id) VALUES ($1,$2,$3,$4)`,
				uuid.NewString(), nullableParent(projectID), node, byNode[node]); err != nil {
				return err
			}
		}
		return nil
	})
	if isFKViolation(err) { // agent_id / project_id 不存在
		return ErrNotFound
	}
	return err
}

// ListBindings：projectID 空串 = 全局默认，否则该项目的覆盖绑定。
func (pg *Pg) ListBindings(ctx context.Context, projectID string) ([]PipelineBinding, error) {
	var q string
	var args []any
	if projectID == "" {
		q = `SELECT id,project_id,node,agent_id,created_at FROM pipeline_bindings WHERE project_id IS NULL ORDER BY created_at`
	} else {
		q = `SELECT id,project_id,node,agent_id,created_at FROM pipeline_bindings WHERE project_id=$1 ORDER BY created_at`
		args = append(args, projectID)
	}
	rows, err := pg.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectBindings(rows)
}

// ListAllBindings 全部绑定（默认 + 所有项目覆盖）单查询带回，按创建时间升序。
func (pg *Pg) ListAllBindings(ctx context.Context) ([]PipelineBinding, error) {
	rows, err := pg.pool.Query(ctx, `SELECT id,project_id,node,agent_id,created_at FROM pipeline_bindings ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectBindings(rows)
}

// collectBindings 公共行扫描（ListBindings / ListAllBindings 共用）。
func collectBindings(rows pgx.Rows) ([]PipelineBinding, error) {
	out := make([]PipelineBinding, 0)
	for rows.Next() {
		var b PipelineBinding
		var pid sql.NullString
		if err := rows.Scan(&b.ID, &pid, &b.Node, &b.AgentID, &b.CreatedAt); err != nil {
			return nil, err
		}
		b.ProjectID = pid.String
		out = append(out, b)
	}
	return out, rows.Err()
}
