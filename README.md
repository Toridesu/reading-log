# 読書管理アプリ

AWSの設計・構築・運用、CI/CD、脅威検知・対応を学ぶための個人開発。

リポジトリ：[Toridesu/reading-log](https://github.com/Toridesu/reading-log)。進捗は [TODO.md](TODO.md) で管理する。

現在はステップ2まで実装・確認済み。Goのトップ画面、JSONログ、PostgreSQL接続、テーブル作成が動く。次にKeycloakでログインを実装し、その後に読書タスクの操作を追加する。当面はAWSリソースを作成しない。

## 起動

Docker Desktopを起動し、WSLのプロジェクトディレクトリで実行する。必要なものはGo、Docker Compose、Bash、Python 3（初回の設定生成用）。PostgreSQL本体はコンテナで動かす。

```bash
bash scripts/setup-local.sh
docker compose up -d --wait db
bash scripts/dev.sh run -buildvcs=false ./cmd/migrate up
bash scripts/dev.sh run -buildvcs=false ./cmd/web
```

[http://localhost:8080](http://localhost:8080) を開く。アプリの停止は `Ctrl+C`。DBも止める場合は `docker compose stop db`。データは残る。

初回のスクリプトはランダムなDBパスワードを含む `.env` を作る。既存のファイルは上書きせず、Gitにも含めない。`scripts/dev.sh` はこのファイルを読み込んでGoを実行する。

Go 1.27.4は、この環境では `.tools/go` に配置済み。`scripts/go.sh` はこのSDKを優先し、なければPATH上のGoを使う。別環境では [.go-version](.go-version) に記載したGoを[公式手順](https://go.dev/doc/install)で用意する。SDKとキャッシュはGit管理対象外。

## 確認

```bash
# DBなしのテストと静的検証
bash scripts/go.sh test ./...
bash scripts/go.sh vet ./...

# DB起動後：実DBのマイグレーション・制約も検証
bash scripts/dev.sh test ./...

# アプリ起動後
curl -i http://localhost:8080/healthz
curl -i http://localhost:8080/readyz
```

`/healthz` はアプリの稼働確認。`/readyz` はDBへ接続できれば200、接続できなければ503を返す。テーブルが揃っているかはマイグレーションの `status` で確認する。

アプリの標準出力はJSONログ。レスポンスの `X-Request-ID` とログの `request_id` を照合し、`path`、`status`、`duration_ms` を見る。DB障害時には `database_unavailable` を記録する。クエリ文字列、Authorization、Cookie、リクエスト本文は記録しない。

## 設定

設定項目は [.env.example](.env.example)、実際の値はローカルの `.env` に置く。

| 環境変数 | 用途 |
| --- | --- |
| `APP_ADDR` | アプリの待受先。初期値は `127.0.0.1:8080` |
| `DB_PORT` | ホスト側のDBポート。初期値は5432 |
| `POSTGRES_PASSWORD` | DB初期化用の管理者パスワード |
| `APP_DB_PASSWORD` | アプリ専用DBロールのパスワード |
| `DATABASE_URL` | アプリとマイグレーションの接続先 |
| `TEST_DATABASE_URL` | 統合テスト専用DBの接続先 |

`dev.sh` は `.env` の値を読み込むので、待受ポートを変えるときは `.env` を編集する。DBポート変更時は両方の接続URLも揃える。初期化済みのDBのパスワードは、ファイルを編集するだけでは変更されない。[DBの操作・調査手順](docs/runbooks/local-database.md)を参照。

## ファイル配置

```text
cmd/web/                 アプリの起動・設定・正常終了
cmd/migrate/             テーブル変更の適用・状態確認
internal/web/            HTTPルート、ログ、画面、テスト
internal/database/       DB接続とマイグレーション、DBテスト
migrations/              テーブル定義のSQL
compose.yaml             ローカルのPostgreSQL
docker/postgres/         初回のDBユーザー・テスト用DB作成
scripts/                 設定生成とGoの実行補助
docs/                    設計判断・手順・検証記録
TODO.md                  作業順と進捗
```

## 記録

- [フェーズ1の構成案・到達点](docs/phase1-design.md)
- [Go・OIDCの採用理由](docs/adr/0002-go-and-oidc.md)
- [開発環境とインストール対象](docs/runbooks/development-environment.md)
- [DBの操作・調査・削除手順](docs/runbooks/local-database.md)
- [ステップ1の検証結果](docs/verification/01-go-foundation.md)
- [ステップ2の検証結果](docs/verification/02-postgresql.md)

UIは不要なセカンダリラベルや説明を避け、レイアウトで伝える。
