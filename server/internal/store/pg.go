package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

const (
	projectCols  = "id,name,repo_url,default_branch,pinned,created_at,updated_at"
	deliveryCols = "id,project_id,title,description,status,current_stage,pending_gate,fail_count,base_commit,created_at,updated_at"
	stageRunCols = "id,delivery_id,stage,attempt,status,started_at,finished_at"
)

// scan helpers：单行查询与列表路径共用同一份 scan 目标列表（pgx.Rows 满足 pgx.Row）。

func scanProject(row pgx.Row) (*Project, error) {
	p := &Project{}
	if err := row.Scan(&p.ID, &p.Name, &p.RepoURL, &p.DefaultBranch, &p.Pinned, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	return p, nil
}

func scanDelivery(row pgx.Row) (*Delivery, error) {
	d := &Delivery{}
	if err := row.Scan(&d.ID, &d.ProjectID, &d.Title, &d.Description, &d.Status, &d.CurrentStage, &d.PendingGate, &d.FailCount, &d.BaseCommit, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	return d, nil
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

func (pg *Pg) ProjectStats(ctx context.Context, id string) (ProjectStats, error) {
	proj, err := pg.GetProject(ctx, id)
	if err != nil {
		return ProjectStats{}, err
	}
	// Last = max(project.updated_at, max(delivery.updated_at))，无 delivery 时取 project.updated_at。
	s := ProjectStats{Last: proj.UpdatedAt}
	var lastDelivery *time.Time
	err = pg.pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE status='active'),
		        count(*) FILTER (WHERE pending_gate<>''),
		        max(updated_at)
		 FROM deliveries WHERE project_id=$1`, id).
		Scan(&s.Active, &s.Pending, &lastDelivery)
	if err != nil {
		return ProjectStats{}, err
	}
	if lastDelivery != nil && lastDelivery.After(s.Last) {
		s.Last = *lastDelivery
	}
	return s, nil
}

// deliveries

func (pg *Pg) CreateDelivery(ctx context.Context, d *Delivery) error {
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	_, err := pg.pool.Exec(ctx,
		`INSERT INTO deliveries (id,project_id,title,description,status,current_stage,pending_gate,fail_count,base_commit)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		d.ID, d.ProjectID, d.Title, d.Description, d.Status, d.CurrentStage, d.PendingGate, d.FailCount, d.BaseCommit)
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

func (pg *Pg) UpdateDelivery(ctx context.Context, d *Delivery) error {
	err := pg.pool.QueryRow(ctx,
		`UPDATE deliveries SET title=$2,description=$3,status=$4,current_stage=$5,pending_gate=$6,fail_count=$7,base_commit=$8,updated_at=now()
		 WHERE id=$1 RETURNING updated_at`,
		d.ID, d.Title, d.Description, d.Status, d.CurrentStage, d.PendingGate, d.FailCount, d.BaseCommit).
		Scan(&d.UpdatedAt)
	return mapErr(err)
}

// events / artifacts / stage_runs

func (pg *Pg) AppendEvent(ctx context.Context, e *Event) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	payload := e.Payload
	if payload == nil {
		payload = []byte(`{}`) // payload 列 NOT NULL
	}
	_, err := pg.pool.Exec(ctx,
		`INSERT INTO events (id,delivery_id,stage,event_type,payload) VALUES ($1,$2,$3,$4,$5)`,
		e.ID, e.DeliveryID, e.Stage, e.EventType, payload)
	if err != nil {
		return err
	}
	return pg.pool.QueryRow(ctx, `SELECT created_at FROM events WHERE id=$1`, e.ID).Scan(&e.CreatedAt)
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

func (pg *Pg) SaveArtifact(ctx context.Context, a *Artifact) error {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	_, err := pg.pool.Exec(ctx,
		`INSERT INTO artifacts (id,delivery_id,stage,kind,content) VALUES ($1,$2,$3,$4,$5)`,
		a.ID, a.DeliveryID, a.Stage, a.Kind, a.Content)
	if err != nil {
		return err
	}
	return pg.pool.QueryRow(ctx, `SELECT created_at FROM artifacts WHERE id=$1`, a.ID).Scan(&a.CreatedAt)
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

func (pg *Pg) LatestStageRun(ctx context.Context, deliveryID, stage string) (*StageRun, error) {
	return scanStageRun(pg.pool.QueryRow(ctx,
		`SELECT `+stageRunCols+` FROM stage_runs WHERE delivery_id=$1 AND stage=$2 ORDER BY started_at DESC LIMIT 1`,
		deliveryID, stage))
}
