package db

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/stretchr/testify/require"
)

// TestMigratePurgeGlobalBindings（0009，INFERA-180）：迁到 0008 后种
// 「全局默认绑定行（project_id NULL）+ 项目绑定行」，升到最新 → 全局行被
// 清除、项目行保留；0009 up SQL 重复执行幂等（DELETE 无害）。
func TestMigratePurgeGlobalBindings(t *testing.T) {
	adminURL := os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("TEST_DATABASE_URL 未设置")
	}

	scratch := fmt.Sprintf("purge_scratch_%d", os.Getpid())
	scratchURL, err := withDatabase(adminURL, scratch)
	require.NoError(t, err)

	admin, err := Connect(context.Background(), adminURL)
	require.NoError(t, err)
	_, err = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+scratch+" WITH (FORCE)")
	require.NoError(t, err)
	_, err = admin.Exec(context.Background(), "CREATE DATABASE "+scratch)
	require.NoError(t, err)
	defer admin.Exec(context.Background(), "DROP DATABASE "+scratch+" WITH (FORCE)")

	// 只应用前 8 个迁移（0009 尚未跑）
	src, err := iofs.New(migrationsFS, "migrations")
	require.NoError(t, err)
	m, err := migrate.NewWithSourceInstance("iofs", src, toPgxURL(scratchURL))
	require.NoError(t, err)
	require.NoError(t, m.Steps(8))
	_, _ = m.Close()

	pool, err := Connect(context.Background(), scratchURL)
	require.NoError(t, err)
	defer pool.Close()
	ctx := context.Background()

	// 种数据：agent + 项目 + 全局默认绑定行 + 项目绑定行
	seed := []string{
		`INSERT INTO agents (id, name, runner) VALUES ('11111111-1111-1111-1111-111111111111', 'a', 'local')`,
		`INSERT INTO projects (id, name) VALUES ('22222222-2222-2222-2222-222222222222', 'p')`,
		`INSERT INTO pipeline_bindings (id, project_id, node, agent_id)
		 VALUES ('33333333-3333-3333-3333-333333333333', NULL, 'spec', '11111111-1111-1111-1111-111111111111')`,
		`INSERT INTO pipeline_bindings (id, project_id, node, agent_id)
		 VALUES ('44444444-4444-4444-4444-444444444444', '22222222-2222-2222-2222-222222222222', 'spec', '11111111-1111-1111-1111-111111111111')`,
	}
	for _, q := range seed {
		_, err := pool.Exec(ctx, q)
		require.NoError(t, err)
	}

	// 升到最新：0009 应用 → 全局行清除、项目行保留
	require.NoError(t, Migrate(scratchURL))
	var global, project int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM pipeline_bindings WHERE project_id IS NULL`).Scan(&global))
	require.Equal(t, 0, global, "0009 必须清除全部全局默认绑定行")
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM pipeline_bindings WHERE project_id IS NOT NULL`).Scan(&project))
	require.Equal(t, 1, project, "项目级绑定不受 0009 影响")

	// 幂等：直接重复执行 0009 up 内容，无错且行数不变
	upSQL, err := migrationsFS.ReadFile("migrations/0009_purge_global_bindings.up.sql")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(upSQL))
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM pipeline_bindings WHERE project_id IS NULL`).Scan(&global))
	require.Equal(t, 0, global)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM pipeline_bindings WHERE project_id IS NOT NULL`).Scan(&project))
	require.Equal(t, 1, project)
}
