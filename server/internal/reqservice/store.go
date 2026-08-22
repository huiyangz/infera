package reqservice

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tokfinity/infera/internal/flow"
)

// ErrNotFound 资源（需求 / 卡片 / 项目）不存在。
var ErrNotFound = errors.New("reqservice: 资源不存在")

// isFKViolation 识别 PG 外键冲突（23503）——project_settings 指向不存在的项目。
func isFKViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// mapErr 把 pgx.ErrNoRows 映射为 ErrNotFound。
func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// insertRequirement 落一行需求（含轮询游标零值列）并回填时间戳。
func (s *Service) insertRequirement(ctx context.Context, r *flow.Requirement) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO requirements (id, title, description, acceptance_criteria, source, priority,
			acceptors, external_issue_id, external_issue_key, node, pr_url)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING created_at, updated_at`,
		r.ID, r.Title, r.Description, r.AcceptanceCriteria, r.Source, r.Priority,
		r.Acceptors, r.ExternalIssueID, r.ExternalIssueKey, string(r.Node), r.PRURL,
	).Scan(&r.CreatedAt, &r.UpdatedAt)
}

// scanRequirement 是 requirements 行的统一扫描（列顺序与 listRequirements/getRequirement 一致）。
func scanRequirement(row pgx.Row) (*flow.Requirement, error) {
	var r flow.Requirement
	var node string
	err := row.Scan(&r.ID, &r.Title, &r.Description, &r.AcceptanceCriteria, &r.Source, &r.Priority,
		&r.Acceptors, &r.ExternalIssueID, &r.ExternalIssueKey, &node, &r.PRURL, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	r.Node = flow.Node(node)
	return &r, nil
}

const requirementCols = `id, title, description, acceptance_criteria, source, priority,
	acceptors, external_issue_id, external_issue_key, node, pr_url, created_at, updated_at`

// getRequirement 按 id 取需求行。
func (s *Service) getRequirement(ctx context.Context, id string) (*flow.Requirement, error) {
	return scanRequirement(s.pool.QueryRow(ctx,
		`SELECT `+requirementCols+` FROM requirements WHERE id = $1`, id))
}

// listRequirements 全量需求（新→旧）。
func (s *Service) listRequirements(ctx context.Context) ([]flow.Requirement, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+requirementCols+` FROM requirements ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []flow.Requirement
	for rows.Next() {
		var r flow.Requirement
		var node string
		if err := rows.Scan(&r.ID, &r.Title, &r.Description, &r.AcceptanceCriteria, &r.Source, &r.Priority,
			&r.Acceptors, &r.ExternalIssueID, &r.ExternalIssueKey, &node, &r.PRURL, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Node = flow.Node(node)
		out = append(out, r)
	}
	return out, rows.Err()
}

// newID 生成 UUID 主键（gate_cards / audit_log / requirements 共用）。
func newID() string { return uuid.NewString() }

// listPendingCards 某需求的待处理闸门卡（旧 → 新，渲染按到达顺序）。
func (s *Service) listPendingCards(ctx context.Context, requirementID string) ([]GateCard, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, requirement_id::text, kind, status, payload, comment_id, created_at
		FROM gate_cards WHERE requirement_id = $1 AND status = 'pending'
		ORDER BY created_at, id`, requirementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GateCard
	for rows.Next() {
		var c GateCard
		if err := rows.Scan(&c.ID, &c.RequirementID, &c.Kind, &c.Status, &c.Payload, &c.CommentID, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// pendingCardCounts 各需求的待处理卡计数（一次聚合查询，避免列表 N+1）。
func (s *Service) pendingCardCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT requirement_id::text, count(*) FROM gate_cards
		WHERE status = 'pending' GROUP BY requirement_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
