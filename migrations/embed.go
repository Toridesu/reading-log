package migrations

import "embed"

// Files はこのフォルダのSQLを実行ファイルへ埋め込む。実行時にSQLファイルを探す必要がなくなる。
//
//go:embed *.sql
var Files embed.FS
