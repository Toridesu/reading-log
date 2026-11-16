// PostgreSQLへの接続処理。Webアプリとマイグレーションの両方から使う。
package database

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// Open はDB接続を使い回すための「接続プール」を作り、実際に接続できるかも確認する。
func Open(ctx context.Context, connectionString string) (*sql.DB, error) {
	if connectionString == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	requireTLS := true
	if value, present := os.LookupEnv("DATABASE_REQUIRE_TLS"); present {
		var err error
		requireTLS, err = strconv.ParseBool(value)
		if err != nil {
			return nil, errors.New("DATABASE_REQUIRE_TLS must be true or false")
		}
	}
	config, err := connectionConfig(connectionString, requireTLS)
	if err != nil {
		return nil, err
	}
	// pgxをドライバー（GoとPostgreSQLをつなぐ部品）として、標準のdatabase/sqlから使う。
	db := stdlib.OpenDB(*config)
	// 小さなアプリなので同時接続は最大5本に抑える。
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("database connection failed; check DB availability, credentials and TLS certificates")
	}
	return db, nil
}

// 通常は証明書とホスト名を検証する。平文のローカルDBだけ、呼び出し側の設定で例外にする。
func connectionConfig(connectionString string, requireTLS bool) (*pgx.ConnConfig, error) {
	config, err := pgx.ParseConfig(connectionString)
	if err != nil {
		// pgxのエラーには接続URLやパスワードが含まれ得るため、そのまま返さない。
		return nil, errors.New("DATABASE_URL is invalid")
	}
	if requireTLS {
		if config.TLSConfig == nil || config.TLSConfig.InsecureSkipVerify || config.TLSConfig.ServerName == "" {
			return nil, errors.New("DATABASE_URL must verify the certificate and hostname (sslmode=verify-full)")
		}
		// prefer/allowの平文への切替や、複数ホスト側の設定漏れも受け付けない。
		for _, fallback := range config.Fallbacks {
			if fallback.TLSConfig == nil || fallback.TLSConfig.InsecureSkipVerify || fallback.TLSConfig.ServerName == "" {
				return nil, errors.New("DATABASE_URL fallbacks must use verified TLS")
			}
		}
	}
	return config, nil
}
