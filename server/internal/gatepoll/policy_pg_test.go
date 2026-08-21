package gatepoll

// SettingsPolicy 落库测试（INFERA-40 T08）：读 project_settings 表的部署级
// 单行策略，三档解析 + 缺省回落 manual。

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/flow"
)

// seedProjectSettings 落一行项目 + 一行设置（直写 SQL——绕开 reqservice 的
// 写入校验也是本测试的输入面之一，见缺省回落用例）。
func seedProjectSettings(t *testing.T, s *PgStore, mode string, threshold int) {
	t.Helper()
	_, err := s.pool.Exec(context.Background(), `
		WITH p AS (INSERT INTO projects (id, name) VALUES (gen_random_uuid(), 'demo') RETURNING id)
		INSERT INTO project_settings (project_id, merge_policy_mode, merge_diff_line_threshold)
		SELECT id, $1, $2 FROM p`, mode, threshold)
	require.NoError(t, err)
}

// TestSettingsPolicyResolvesStoredModes：表里三档各自解析（threshold 带阈值）。
// 部署级单行语义——每档重种唯一一行。
func TestSettingsPolicyResolvesStoredModes(t *testing.T) {
	s := testPgStore(t)
	pol := NewSettingsPolicy(s.pool)
	ctx := context.Background()

	for _, tc := range []struct {
		mode      string
		threshold int
		want      flow.MergePolicy
	}{
		{"manual", 0, flow.MergePolicy{Mode: flow.MergeManual}},
		{"auto_pass", 0, flow.MergePolicy{Mode: flow.MergeAutoPass}},
		{"threshold", 300, flow.MergePolicy{Mode: flow.MergeThreshold, DiffLineThreshold: 300}},
	} {
		_, err := s.pool.Exec(ctx, `TRUNCATE project_settings`)
		require.NoError(t, err)
		seedProjectSettings(t, s, tc.mode, tc.threshold)

		got, err := pol.MergePolicy(ctx, flow.Requirement{})
		require.NoError(t, err)
		require.Equal(t, tc.want, got, "mode=%s threshold=%d", tc.mode, tc.threshold)
	}
}

// TestSettingsPolicyFallsBackToManual：缺省回落——无设置行 → manual；
// 直写 SQL 绕过 SetMergePolicy 校验落进来的损坏档位（未知 mode /
// threshold 档缺阈值）→ 同样回落 manual：自动合并是风险动作，数据异常时
// 选最保守的行为（不自动合并），而不是让轮询反复失败。
func TestSettingsPolicyFallsBackToManual(t *testing.T) {
	s := testPgStore(t)
	pol := NewSettingsPolicy(s.pool)
	ctx := context.Background()

	// 无行：未设置过。
	got, err := pol.MergePolicy(ctx, flow.Requirement{})
	require.NoError(t, err)
	require.Equal(t, flow.DefaultMergePolicy(), got, "无设置行回落默认手动档")

	for _, tc := range []struct {
		name      string
		mode      string
		threshold int
	}{
		{"未知档位", "yolo", 0},
		{"threshold 档缺阈值", "threshold", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.pool.Exec(ctx, `TRUNCATE project_settings`)
			require.NoError(t, err)
			seedProjectSettings(t, s, tc.mode, tc.threshold)

			got, err := pol.MergePolicy(ctx, flow.Requirement{})
			require.NoError(t, err)
			require.Equal(t, flow.DefaultMergePolicy(), got)
		})
	}
}
