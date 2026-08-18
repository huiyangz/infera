package db

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrate(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL 未设置")
	}
	if err := Migrate(url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// 幂等
	if err := Migrate(url); err != nil {
		t.Fatalf("migrate twice: %v", err)
	}
}

// TestMigrateParentIndex：拆分父子查询（ListChildDeliveries / 合并循环）依赖
// deliveries(parent_id) 索引——迁移后必须存在。
func TestMigrateParentIndex(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL 未设置")
	}
	require.NoError(t, Migrate(url))
	pool, err := Connect(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM pg_indexes WHERE tablename='deliveries' AND indexname='idx_deliveries_parent'`).Scan(&n))
	require.Equal(t, 1, n, "deliveries(parent_id) 索引必须存在（拆分子需求查询路径）")
}
