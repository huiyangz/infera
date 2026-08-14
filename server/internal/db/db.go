package db

import (
	"context"
	"embed"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Connect 建立 pgx 连接池并 ping 验证（5s 超时）。
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	ctx2, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(ctx2); err != nil {
		return nil, err
	}
	return pool, nil
}

// Migrate 把 schema 迁到最新（iofs 内嵌）。幂等。
func Migrate(url string) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	d, err := migrate.NewWithSourceInstance("iofs", src, toPgxURL(url))
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}

// golang-migrate 的 pgx v5 driver 用 "pgx5://" scheme。
func toPgxURL(u string) string { return strings.Replace(u, "postgres://", "pgx5://", 1) }
