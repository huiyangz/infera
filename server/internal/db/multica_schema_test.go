package db

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMigrateMulticaSyncFromScratch：multica 同步迁移（projects/deliveries 的外部来源
// 列 + 部分唯一索引）在全新数据库上从零执行成功，且可重复执行（幂等）。
// 契约列与唯一索引是 T03 同步链路的存储面（INFERA-79 T02 冻结）。
func TestMigrateMulticaSyncFromScratch(t *testing.T) {
	adminURL := os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("TEST_DATABASE_URL 未设置")
	}

	scratch := fmt.Sprintf("multica_scratch_%d", os.Getpid())
	scratchURL, err := withDatabase(adminURL, scratch)
	require.NoError(t, err)

	admin, err := Connect(context.Background(), adminURL)
	require.NoError(t, err)
	_, err = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+scratch+" WITH (FORCE)")
	require.NoError(t, err)
	_, err = admin.Exec(context.Background(), "CREATE DATABASE "+scratch)
	require.NoError(t, err)
	defer admin.Exec(context.Background(), "DROP DATABASE "+scratch+" WITH (FORCE)")

	// 从零迁移 + 幂等重跑
	require.NoError(t, Migrate(scratchURL))
	require.NoError(t, Migrate(scratchURL))

	pool, err := Connect(context.Background(), scratchURL)
	require.NoError(t, err)
	defer pool.Close()

	// 外部来源契约列存在
	for _, col := range []struct{ tbl, col string }{
		{"projects", "multica_project_id"},
		{"projects", "multica_synced_at"},
		{"deliveries", "multica_issue_id"},
		{"deliveries", "multica_issue_key"},
		{"deliveries", "multica_synced_at"},
		{"deliveries", "assignee"},
		{"deliveries", "priority"},
	} {
		var ok bool
		require.NoError(t, pool.QueryRow(context.Background(),
			`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name=$1 AND column_name=$2)`,
			col.tbl, col.col).Scan(&ok),
			"查列 %s.%s 失败", col.tbl, col.col)
		require.True(t, ok, "缺契约列 %s.%s", col.tbl, col.col)
	}

	// 外部 ID 部分唯一索引存在（同步 upsert 的 ON CONFLICT 目标）
	for _, idx := range []struct{ tbl, idx string }{
		{"projects", "idx_projects_multica"},
		{"deliveries", "idx_deliveries_multica"},
	} {
		var n int
		require.NoError(t, pool.QueryRow(context.Background(),
			`SELECT count(*) FROM pg_indexes WHERE tablename=$1 AND indexname=$2`,
			idx.tbl, idx.idx).Scan(&n))
		require.Equal(t, 1, n, "唯一索引 %s 必须存在", idx.idx)
	}
}
