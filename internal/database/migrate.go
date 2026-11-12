// gooseというライブラリに、SQLファイルとDB変更の管理方法を設定する。
package database

import (
	"database/sql"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"readinglog/migrations"
)

// Migrator は適用済みSQLを記録する仕組みを用意する。実際の適用は呼び出し元で行う。
func Migrator(db *sql.DB) (*goose.Provider, error) {
	// 2つのコマンドが同時にテーブルを変更しないよう、DBのロックを使う。
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectPostgres, db, migrations.Files, goose.WithSessionLocker(locker))
}
