package gatepoll

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tokfinity/infera/internal/flow"
)

// SettingsPolicy 是 MergePolicyResolver 的 project_settings 表实现（FR-6，
// INFERA-40 T08）：部署级单行语义——requirements 冻结 schema 无项目关联列，
// 本期单租户，表中唯一一行即部署策略，交给装配层（T07）接线替代 StaticPolicy。
type SettingsPolicy struct {
	pool *pgxpool.Pool
}

// NewSettingsPolicy 基于连接池构造。
func NewSettingsPolicy(pool *pgxpool.Pool) *SettingsPolicy {
	return &SettingsPolicy{pool: pool}
}

var _ MergePolicyResolver = (*SettingsPolicy)(nil)

// MergePolicy 读部署级策略（需求参数忽略——单租户无按需求解析）。
// 读不出有效策略（无设置行 / 直写 SQL 绕过 SetMergePolicy 校验的损坏档位）
// 一律回落手动档：自动合并是风险动作，数据异常时选最保守的行为——
// 不自动合并、轮询不反复失败（合并卡本身就是人的逃生口）。
// ORDER BY project_id 让多行脏数据下的取行可复现（正常部署恰好一行）。
func (s *SettingsPolicy) MergePolicy(ctx context.Context, _ flow.Requirement) (flow.MergePolicy, error) {
	var mode string
	var threshold int
	err := s.pool.QueryRow(ctx, `
		SELECT merge_policy_mode, merge_diff_line_threshold FROM project_settings
		ORDER BY project_id LIMIT 1`).Scan(&mode, &threshold)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return flow.DefaultMergePolicy(), nil // 未设置：默认手动档
		}
		return flow.MergePolicy{}, err
	}
	p := flow.MergePolicy{Mode: flow.MergePolicyMode(mode), DiffLineThreshold: threshold}
	if err := p.Validate(); err != nil {
		return flow.DefaultMergePolicy(), nil // 损坏档位：回落手动（见上）
	}
	return p, nil
}
