package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// Open は接続プールを作り、実際に接続できることまで確認する。
func Open(ctx context.Context, connectionString string) (*sql.DB, error) {
	if connectionString == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	config, err := pgx.ParseConfig(connectionString)
	if err != nil {
		// パースエラーに接続URLやパスワードが含まれる場合があるので、そのまま返さない。
		return nil, errors.New("DATABASE_URL is invalid")
	}
	db := stdlib.OpenDB(*config)
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("database connection failed; check DB availability and credentials")
	}
	return db, nil
}
