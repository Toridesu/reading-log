package database

import (
	"database/sql"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"readinglog/migrations"
)

// Migrator はSQLの適用履歴を管理する。ロックで同時実行を避ける。
func Migrator(db *sql.DB) (*goose.Provider, error) {
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectPostgres, db, migrations.Files, goose.WithSessionLocker(locker))
}
