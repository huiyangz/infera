package db

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// TestMigrateLabelsFromScratch：标签库迁移（labels / delivery_labels）在全新
// 数据库上从零执行成功、重复执行幂等（重启幂等），且 down 可执行——up→down
// 往返后表消失，再 up 表复原。表结构与幂等键（labels.external_label_id 部分
// 唯一索引）是 INFERA-218 T01 冻结的契约存储面。
func TestMigrateLabelsFromScratch(t *testing.T) {
	adminURL := os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("TEST_DATABASE_URL 未设置")
	}

	scratch := fmt.Sprintf("labels_scratch_%d", os.Getpid())
	scratchURL, err := withDatabase(adminURL, scratch)
	require.NoError(t, err)

	admin, err := Connect(context.Background(), adminURL)
	require.NoError(t, err)
	_, err = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+scratch+" WITH (FORCE)")
	require.NoError(t, err)
	_, err = admin.Exec(context.Background(), "CREATE DATABASE "+scratch)
	require.NoError(t, err)
	defer admin.Exec(context.Background(), "DROP DATABASE "+scratch+" WITH (FORCE)")

	// 从零迁移 + 重启幂等（重复执行）
	require.NoError(t, Migrate(scratchURL))
	require.NoError(t, Migrate(scratchURL))

	pool, err := Connect(context.Background(), scratchURL)
	require.NoError(t, err)
	defer pool.Close()
	requireLabelsSchema(t, pool)

	// down 一步（回滚 0010）：表与索引必须干净移除。
	d, err := migrator(scratchURL)
	require.NoError(t, err)
	require.NoError(t, d.Steps(-1))
	d.Close()
	requireLabelsSchemaGone(t, pool)

	// 再 up：结构复原（up/down 可反复执行）。
	require.NoError(t, Migrate(scratchURL))
	requireLabelsSchema(t, pool)
}

// requireLabelsSchema 断言标签契约面在位：两张表、契约列、
// labels.external_label_id 部分唯一索引（同步 upsert 的 ON CONFLICT 目标）。
func requireLabelsSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	for _, tbl := range []string{"labels", "delivery_labels"} {
		var ok bool
		require.NoError(t, pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+tbl).Scan(&ok),
			"查表 %s 失败", tbl)
		require.True(t, ok, "迁移后缺表 %s", tbl)
	}
	for _, col := range []struct{ tbl, col string }{
		{"labels", "name"},
		{"labels", "color"},
		{"labels", "external_label_id"},
		{"labels", "created_at"},
		{"labels", "updated_at"},
		{"delivery_labels", "delivery_id"},
		{"delivery_labels", "label_id"},
	} {
		var ok bool
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name=$1 AND column_name=$2)`,
			col.tbl, col.col).Scan(&ok), "查列 %s.%s 失败", col.tbl, col.col)
		require.True(t, ok, "缺契约列 %s.%s", col.tbl, col.col)
	}
	var n int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_indexes WHERE tablename='labels' AND indexname='idx_labels_external'`).Scan(&n))
	require.Equal(t, 1, n, "labels(external_label_id) 部分唯一索引必须存在（幂等键）")
}

// requireLabelsSchemaGone 断言 down 之后标签面整体移除。
func requireLabelsSchemaGone(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	for _, tbl := range []string{"labels", "delivery_labels"} {
		var ok bool
		require.NoError(t, pool.QueryRow(context.Background(),
			`SELECT to_regclass($1) IS NOT NULL`, "public."+tbl).Scan(&ok))
		require.False(t, ok, "down 后表 %s 应被移除", tbl)
	}
}
