package store

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"slices"
	"time"

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
		total, active, queued, completed, blocked, cancelled, pending int
		synced                                                        sql.NullTime
		exists                                                        bool
	)
	err := pg.pool.QueryRow(ctx,
		`SELECT count(*),
		        count(*) FILTER (WHERE status='active'),
		        count(*) FILTER (WHERE status='queued'),
		        count(*) FILTER (WHERE status='completed'),
		        count(*) FILTER (WHERE status='blocked'),
		        count(*) FILTER (WHERE status='cancelled'),
		        count(*) FILTER (WHERE pending_gate<>'' AND status<>'completed'),
		        (SELECT external_synced_at FROM projects WHERE id=$1),
		        EXISTS(SELECT 1 FROM projects WHERE id=$1)
		 FROM deliveries WHERE project_id=$1`, id).
		Scan(&total, &active, &queued, &completed, &blocked, &cancelled, &pending, &synced, &exists)
	if err != nil {
		return RequirementStats{}, err
	}
	if !exists {
		return RequirementStats{}, ErrNotFound
	}
	s := RequirementStats{
		ProjectID:        id,
		RequirementTotal: total,
		ByStatus:         map[string]int{"active": active, "queued": queued, "completed": completed, "blocked": blocked, "cancelled": cancelled},
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

// ListDeliveriesByLabelNames 跨项目按标签名取交付（需求发现视图，语义与
// Memory 一致）：挂有 names 中任一标签即命中（OR），子查询 IN 天然去重
// （同一交付多标签命中只出一行），按 updated_at 降序（created_at/id 升序
// 保持稳定）。names 空 = ANY 空数组自然空集。
func (pg *Pg) ListDeliveriesByLabelNames(ctx context.Context, names []string) ([]Delivery, error) {
	rows, err := pg.pool.Query(ctx,
		`SELECT `+deliveryCols+` FROM deliveries WHERE id IN (
			SELECT dl.delivery_id FROM delivery_labels dl JOIN labels l ON l.id = dl.label_id
			WHERE l.name = ANY($1))
		 ORDER BY updated_at DESC, created_at, id`, names)
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

// labels（标签库，语义与 Memory 一致）

const labelCols = "id,name,color,external_label_id,created_at,updated_at"

func scanLabel(row pgx.Row) (*Label, error) {
	l := &Label{}
	if err := row.Scan(&l.ID, &l.Name, &l.Color, &l.ExternalLabelID, &l.CreatedAt, &l.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	return l, nil
}

// labelByID 回读整行（Create/Upsert 插入后填充时间戳）。
func (pg *Pg) labelByID(ctx context.Context, id string) (*Label, error) {
	return scanLabel(pg.pool.QueryRow(ctx, `SELECT `+labelCols+` FROM labels WHERE id=$1`, id))
}

// CreateLabel 插入标签（本地标签：ExternalLabelID 空 = 不参与唯一性）；
// 外部 ID 已被占用（23505）→ ErrConflict。
func (pg *Pg) CreateLabel(ctx context.Context, l *Label) error {
	if l.ID == "" {
		l.ID = uuid.NewString()
	}
	err := pg.pool.QueryRow(ctx,
		`INSERT INTO labels (id,name,color,external_label_id) VALUES ($1,$2,$3,$4) RETURNING created_at, updated_at`,
		l.ID, l.Name, l.Color, l.ExternalLabelID).
		Scan(&l.CreatedAt, &l.UpdatedAt)
	if isUnique(err) {
		return ErrConflict
	}
	return mapErr(err)
}

// UpsertLabelByExternalID 按 上游标签 ID 幂等导入（同步链路唯一入口，语义与
// Memory 一致）：ON CONFLICT 命中部分唯一索引（空串不参与唯一性）→ 只更新
// name/color，重复执行不产生重复行。RETURNING id 两个分支都回行 ID，回读
// 整行填充结构体。ExternalLabelID 为空 → ErrInvalid。
func (pg *Pg) UpsertLabelByExternalID(ctx context.Context, l *Label) error {
	if l.ExternalLabelID == "" {
		return ErrInvalid
	}
	if l.ID == "" {
		l.ID = uuid.NewString()
	}
	var id string
	err := pg.pool.QueryRow(ctx,
		`INSERT INTO labels (id,name,color,external_label_id)
		 VALUES ($1,$2,$3,$4)
		 ON CONFLICT (external_label_id) WHERE external_label_id <> ''
		 DO UPDATE SET name=EXCLUDED.name, color=EXCLUDED.color, updated_at=now()
		 RETURNING id`,
		l.ID, l.Name, l.Color, l.ExternalLabelID).Scan(&id)
	if err != nil {
		return mapErr(err)
	}
	got, err := pg.labelByID(ctx, id)
	if err != nil {
		return err
	}
	*l = *got
	return nil
}

func (pg *Pg) ListLabels(ctx context.Context) ([]Label, error) {
	rows, err := pg.pool.Query(ctx, `SELECT `+labelCols+` FROM labels ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Label, 0)
	for rows.Next() {
		l, err := scanLabel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

// AttachLabel 挂标幂等：ON CONFLICT DO NOTHING，重复挂同一标签不产生重复
// 关联行。交付或标签不存在（FK 23503）→ ErrNotFound。
func (pg *Pg) AttachLabel(ctx context.Context, deliveryID, labelID string) error {
	_, err := pg.pool.Exec(ctx,
		`INSERT INTO delivery_labels (delivery_id,label_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		deliveryID, labelID)
	if isFKViolation(err) {
		return ErrNotFound
	}
	return err
}

// DetachLabel 摘除交付的标签关联；关联本就不存在 → ErrNotFound。
func (pg *Pg) DetachLabel(ctx context.Context, deliveryID, labelID string) error {
	tag, err := pg.pool.Exec(ctx,
		`DELETE FROM delivery_labels WHERE delivery_id=$1 AND label_id=$2`, deliveryID, labelID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (pg *Pg) ListDeliveryLabels(ctx context.Context, deliveryID string) ([]Label, error) {
	rows, err := pg.pool.Query(ctx,
		`SELECT l.id,l.name,l.color,l.external_label_id,l.created_at,l.updated_at
		 FROM labels l JOIN delivery_labels dl ON dl.label_id = l.id
		 WHERE dl.delivery_id=$1 ORDER BY l.name`, deliveryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Label, 0)
	for rows.Next() {
		l, err := scanLabel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

// LabelsByDeliveryID 批量取多个交付挂的标签（任务列表一次装配，免 N+1），
// 键 = deliveryID；无标签的交付不出现在结果里。
func (pg *Pg) LabelsByDeliveryID(ctx context.Context, deliveryIDs []string) (map[string][]Label, error) {
	out := make(map[string][]Label, len(deliveryIDs))
	if len(deliveryIDs) == 0 {
		return out, nil
	}
	rows, err := pg.pool.Query(ctx,
		`SELECT dl.delivery_id, l.id,l.name,l.color,l.external_label_id,l.created_at,l.updated_at
		 FROM labels l JOIN delivery_labels dl ON dl.label_id = l.id
		 WHERE dl.delivery_id = ANY($1) ORDER BY l.name`, deliveryIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var deliveryID string
		var l Label
		if err := rows.Scan(&deliveryID, &l.ID, &l.Name, &l.Color, &l.ExternalLabelID, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, err
		}
		out[deliveryID] = append(out[deliveryID], l)
	}
	return out, rows.Err()
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

// ProjectStageRuns 项目维度 agent 执行时序（语义与 Memory 一致）：一条 JOIN
// 查询（deliveries → stage_runs → pipeline_bindings → agents）取明细，
// started_at 倒序（并列按 attempt、id 倒序）LIMIT 截窗，agent 名经
// LEFT JOIN 带回（node=stage，未绑定 → NULL）；聚合在 Go 侧走共用
// aggregateByStage。EXISTS 先行兜底项目存在性（空项目与 404 区分）。
func (pg *Pg) ProjectStageRuns(ctx context.Context, projectID string) (ProjectStageRuns, error) {
	var exists bool
	if err := pg.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1)`, projectID).Scan(&exists); err != nil {
		return ProjectStageRuns{}, err
	}
	if !exists {
		return ProjectStageRuns{}, ErrNotFound
	}
	rows, err := pg.pool.Query(ctx,
		`SELECT sr.id, sr.delivery_id, d.title, d.external_issue_key, sr.stage, sr.attempt, sr.status,
		        a.name, sr.started_at, sr.finished_at
		 FROM stage_runs sr
		 JOIN deliveries d ON d.id = sr.delivery_id
		 LEFT JOIN pipeline_bindings pb ON pb.project_id = d.project_id AND pb.node = sr.stage
		 LEFT JOIN agents a ON a.id = pb.agent_id
		 WHERE d.project_id = $1
		 ORDER BY sr.started_at DESC, sr.attempt DESC, sr.id DESC
		 LIMIT $2`, projectID, stageRunsDetailLimit)
	if err != nil {
		return ProjectStageRuns{}, err
	}
	defer rows.Close()
	details := make([]StageRunDetail, 0)
	for rows.Next() {
		var r StageRunDetail
		var agent sql.NullString
		if err := rows.Scan(&r.ID, &r.DeliveryID, &r.Title, &r.ExternalIssueKey, &r.Stage, &r.Attempt, &r.Status,
			&agent, &r.StartedAt, &r.FinishedAt); err != nil {
			return ProjectStageRuns{}, err
		}
		if agent.Valid {
			name := agent.String
			r.AgentName = &name
		}
		r.DurationMS = stageRunDurationMS(r.StartedAt, r.FinishedAt)
		details = append(details, r)
	}
	return ProjectStageRuns{ProjectID: projectID, Runs: details, ByStage: aggregateByStage(details)}, rows.Err()
}

// AgentActivity 跨项目 agent 执行时序聚合（语义与 Memory 一致）：一条 JOIN
// 查询（stage_runs → deliveries → 项目/全局两级绑定 → agents）取 [from,to)
// 内原始行，归属 agent 经 LEFT JOIN 带回——项目绑定优先（COALESCE），全局
// 兜底，无绑定 → NULL → unbound；分桶走共用 assembleAgentActivity。半开
// 区间：started_at == from 计入、== to 剔除。
func (pg *Pg) AgentActivity(ctx context.Context, from, to time.Time, bucketMinutes int) ([]AgentActivitySeries, error) {
	rows, err := pg.pool.Query(ctx,
		`SELECT a.id, a.name, sr.started_at
		   FROM stage_runs sr
		   JOIN deliveries d ON d.id = sr.delivery_id
		   LEFT JOIN pipeline_bindings pb_project ON pb_project.project_id = d.project_id AND pb_project.node = sr.stage
		   LEFT JOIN pipeline_bindings pb_global ON pb_global.project_id IS NULL AND pb_global.node = sr.stage
		   LEFT JOIN agents a ON a.id = COALESCE(pb_project.agent_id, pb_global.agent_id)
		  WHERE sr.started_at >= $1 AND sr.started_at < $2`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	raw := make([]agentActivityRow, 0)
	for rows.Next() {
		var r agentActivityRow
		var id, name sql.NullString
		if err := rows.Scan(&id, &name, &r.StartedAt); err != nil {
			return nil, err
		}
		if id.Valid && name.Valid {
			r.AgentID, r.AgentName = id.String, name.String
		} else {
			r.AgentName = unboundAgentName
		}
		raw = append(raw, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return assembleAgentActivity(raw, from, to, bucketMinutes)
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

// UpsertBinding 按 (project,node) 幂等覆盖（项目级专用；空 ProjectID → ErrInvalid）。
func (pg *Pg) UpsertBinding(ctx context.Context, b *PipelineBinding) error {
	if b.ProjectID == "" {
		return ErrInvalid
	}
	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	_, err := pg.pool.Exec(ctx,
		`INSERT INTO pipeline_bindings (id,project_id,node,agent_id) VALUES ($1,$2,$3,$4)
		 ON CONFLICT (project_id,node) WHERE project_id IS NOT NULL DO UPDATE SET agent_id=EXCLUDED.agent_id`,
		b.ID, b.ProjectID, b.Node, b.AgentID)
	if isFKViolation(err) { // agent_id / project_id 不存在
		return ErrNotFound
	}
	return err
}

// DeleteBinding 删项目的某节点绑定（项目级专用；空 projectID → ErrInvalid）。
func (pg *Pg) DeleteBinding(ctx context.Context, projectID, node string) error {
	if projectID == "" {
		return ErrInvalid
	}
	tag, err := pg.pool.Exec(ctx, `DELETE FROM pipeline_bindings WHERE project_id=$1 AND node=$2`, projectID, node)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ReplaceBindings 单事务原子替换某项目的全部绑定：先删旧、再按节点序写入新集合，
// 任一步失败整体回滚（调用方拿到错误时原状态一字不差）。空 projectID → ErrInvalid。
// 按节点序插入使失败位置确定：排在失败项之前的写入已真实落过，回滚覆盖的正是半写场景。
func (pg *Pg) ReplaceBindings(ctx context.Context, projectID string, byNode map[string]string) error {
	if projectID == "" {
		return ErrInvalid
	}
	err := pgx.BeginFunc(ctx, pg.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM pipeline_bindings WHERE project_id=$1`, projectID); err != nil {
			return err
		}
		for _, node := range slices.Sorted(maps.Keys(byNode)) {
			if _, err := tx.Exec(ctx,
				`INSERT INTO pipeline_bindings (id,project_id,node,agent_id) VALUES ($1,$2,$3,$4)`,
				uuid.NewString(), projectID, node, byNode[node]); err != nil {
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

// ListBindings：某项目的绑定（项目级专用；空 projectID → ErrInvalid）。
func (pg *Pg) ListBindings(ctx context.Context, projectID string) ([]PipelineBinding, error) {
	if projectID == "" {
		return nil, ErrInvalid
	}
	rows, err := pg.pool.Query(ctx,
		`SELECT id,project_id,node,agent_id,created_at FROM pipeline_bindings WHERE project_id=$1 ORDER BY created_at`,
		projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectBindings(rows)
}

// ListAllBindings 全部项目的绑定单查询带回，按创建时间升序。
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
