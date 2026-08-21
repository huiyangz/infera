package db

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMigrateFlowFromScratch：flow 迁移（requirements / gate_cards / audit_log /
// project_settings）在全新数据库上从零执行成功（AC：全新 postgres 从零执行）。
//
// 用一次性 scratch 库验证，不动共享的 infera_v2；结束后整库删除。
func TestMigrateFlowFromScratch(t *testing.T) {
	adminURL := os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("TEST_DATABASE_URL 未设置")
	}

	scratch := fmt.Sprintf("flow_scratch_%d", os.Getpid())
	scratchURL, err := withDatabase(adminURL, scratch)
	require.NoError(t, err)

	// 管理连接：建/删 scratch 库
	admin, err := Connect(context.Background(), adminURL)
	require.NoError(t, err)
	_, err = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+scratch+" WITH (FORCE)")
	require.NoError(t, err)
	_, err = admin.Exec(context.Background(), "CREATE DATABASE "+scratch)
	require.NoError(t, err)
	defer admin.Exec(context.Background(), "DROP DATABASE "+scratch+" WITH (FORCE)")

	// 从零迁移 + 幂等
	require.NoError(t, Migrate(scratchURL))
	require.NoError(t, Migrate(scratchURL))

	pool, err := Connect(context.Background(), scratchURL)
	require.NoError(t, err)
	defer pool.Close()

	// 四张表存在，关键契约列在（下游只读消费，schema 以迁移为准）
	checks := []string{
		"requirements",
		"gate_cards",
		"audit_log",
		"project_settings",
	}
	for _, tbl := range checks {
		var ok bool
		require.NoError(t, pool.QueryRow(context.Background(),
			`SELECT to_regclass($1) IS NOT NULL`, "public."+tbl).Scan(&ok),
			"表 %s 必须存在", tbl)
		require.True(t, ok, "迁移后缺表 %s", tbl)
	}
	for _, col := range []struct{ tbl, col string }{
		{"requirements", "node"},
		{"requirements", "poll_last_comment_at"},
		{"requirements", "poll_last_status"},
		{"requirements", "poll_seen_verdict"},
		{"gate_cards", "kind"},
		{"gate_cards", "status"},
		{"project_settings", "merge_policy_mode"},
		{"project_settings", "merge_diff_line_threshold"},
		{"audit_log", "actor"},
	} {
		var ok bool
		require.NoError(t, pool.QueryRow(context.Background(),
			`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name=$1 AND column_name=$2)`,
			col.tbl, col.col).Scan(&ok),
			"查列 %s.%s 失败", col.tbl, col.col)
		require.True(t, ok, "缺契约列 %s.%s", col.tbl, col.col)
	}

	// requirements.node 默认 intake（需求受理起点，DB 层兜底）
	var dflt string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT column_default FROM information_schema.columns
		 WHERE table_name='requirements' AND column_name='node'`).Scan(&dflt))
	require.Contains(t, dflt, "intake", "requirements.node 默认值应为 intake, got %q", dflt)
}

// withDatabase 替换连接串里的库名，生成 scratch 库 URL。
func withDatabase(rawURL, dbName string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	u.Path = "/" + dbName
	return u.String(), nil
}
