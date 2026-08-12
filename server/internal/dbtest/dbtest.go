package dbtest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testDBURL = "postgres://infera:infera@localhost:5433/infera_test?sslmode=disable"

// migrationsDir 从当前工作目录向上查找 go.mod（模块根 = server/），返回其下的 migrations 目录。
// 这样无论测试从哪个 internal/<pkg> 运行都能正确定位迁移文件。
func migrationsDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return "file://" + filepath.Join(dir, "migrations")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod above %s", dir)
		}
		dir = parent
	}
}

// Migrate 对 test 库跑全部迁移。
func Migrate(t *testing.T) {
	t.Helper()
	m, err := migrate.New(migrationsDir(t), testDBURL)
	if err != nil {
		t.Fatalf("migrate new: %v", err)
	}
	if err := m.Up(); err != nil && err.Error() != "no change" {
		t.Fatalf("migrate up: %v", err)
	}
	srcErr, dbErr := m.Close()
	if srcErr != nil {
		t.Fatalf("migrate close source: %v", srcErr)
	}
	if dbErr != nil {
		t.Fatalf("migrate close db: %v", dbErr)
	}
}

// Pool 返回连到 test 库的连接池。
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), testDBURL)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	return pool
}

// Truncate 清空给定表，保证测试间隔离。
func Truncate(t *testing.T, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	for _, tb := range tables {
		if _, err := pool.Exec(context.Background(), fmt.Sprintf("TRUNCATE TABLE %s CASCADE", tb)); err != nil {
			t.Fatalf("truncate %s: %v", tb, err)
		}
	}
}

// SQLTx 保留 sql.DB 句柄别名，供 sqlc 生成的 Queries 在事务里使用。
func SQLTx(t *testing.T, pool *pgxpool.Pool) (*sql.DB, *sql.Tx, func()) {
	t.Helper()
	db, err := sql.Open("pgx", testDBURL)
	if err != nil {
		t.Fatalf("open sql: %v", err)
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	cleanup := func() {
		_ = tx.Rollback()
		_ = db.Close()
	}
	return db, tx, cleanup
}
