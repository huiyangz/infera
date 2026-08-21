package reqservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tokfinity/infera/internal/flow"
)

// AuditEntry 是审计记录的 API 响应形态（只增不改，谁、何时、做了什么）。
type AuditEntry struct {
	ID            string    `json:"id"`
	RequirementID string    `json:"requirement_id"`
	Actor         string    `json:"actor"`
	Action        string    `json:"action"`
	Detail        string    `json:"detail"`
	At            time.Time `json:"at"`
}

// ListAudit 某需求的审计时间线（旧 → 新）。
func (s *Service) ListAudit(ctx context.Context, requirementID string) ([]AuditEntry, error) {
	if _, err := s.getRequirement(ctx, requirementID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, requirement_id::text, actor, action, detail, created_at
		FROM audit_log WHERE requirement_id = $1 ORDER BY created_at, id`, requirementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.RequirementID, &e.Actor, &e.Action, &e.Detail, &e.At); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetMergePolicy 读项目合并策略（FR-6）。未设置过 → 默认手动档。
func (s *Service) GetMergePolicy(ctx context.Context, projectID string) (flow.MergePolicy, error) {
	var mode string
	var threshold int
	err := s.pool.QueryRow(ctx, `
		SELECT merge_policy_mode, merge_diff_line_threshold FROM project_settings
		WHERE project_id = $1`, projectID).Scan(&mode, &threshold)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// 行不存在：项目本身可能也不存在——FK 关系下无设置行不区分二者，
			// 显式查项目存在性，未设置返回默认档。
			if err := s.projectExists(ctx, projectID); err != nil {
				return flow.MergePolicy{}, err
			}
			return flow.DefaultMergePolicy(), nil
		}
		return flow.MergePolicy{}, err
	}
	return flow.MergePolicy{Mode: flow.MergePolicyMode(mode), DiffLineThreshold: threshold}, nil
}

// SetMergePolicy 写项目合并策略：flow.Validate 校验档位语义 → UPSERT。
// 项目不存在（FK 冲突）→ ErrNotFound。
func (s *Service) SetMergePolicy(ctx context.Context, projectID string, p flow.MergePolicy) (flow.MergePolicy, error) {
	if err := p.Validate(); err != nil {
		return flow.MergePolicy{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO project_settings (project_id, merge_policy_mode, merge_diff_line_threshold)
		VALUES ($1, $2, $3)
		ON CONFLICT (project_id) DO UPDATE
		SET merge_policy_mode = EXCLUDED.merge_policy_mode,
		    merge_diff_line_threshold = EXCLUDED.merge_diff_line_threshold,
		    updated_at = now()`,
		projectID, string(p.Mode), p.DiffLineThreshold)
	if err != nil {
		if isFKViolation(err) {
			return flow.MergePolicy{}, fmt.Errorf("%w: 项目不存在", ErrNotFound)
		}
		return flow.MergePolicy{}, err
	}
	return p, nil
}

// projectExists 显式存在性检查。
func (s *Service) projectExists(ctx context.Context, projectID string) error {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT true FROM projects WHERE id = $1`, projectID).Scan(&ok)
	if err != nil {
		return mapErr(err)
	}
	return nil
}
