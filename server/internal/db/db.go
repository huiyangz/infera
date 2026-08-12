package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql 驱动，测试用
)

// Pool 返回一个 pgx 连接池。
func Pool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect db: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return pool, nil
}

// OpenSQL 打开一个 *database/sql 句柄（测试与 migrate 用）。
func OpenSQL(databaseURL string) (*sql.DB, error) {
	return sql.Open("pgx", databaseURL)
}
