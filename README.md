# 読書管理アプリ

AWSの設計・構築・運用、CI/CD、脅威検知・対応を学ぶための個人開発。

リポジトリ：[Toridesu/reading-log](https://github.com/Toridesu/reading-log)。進捗は [TODO.md](TODO.md) で管理する。

現在はステップ4まで実装。Keycloak経由のログイン・ログアウトと、自分の読書タスクの作成・一覧・編集・削除が動く。次はログ調査とDBのバックアップ・復元を試す。当面はAWSリソースを作成しない。

## 起動

Docker Desktopを起動し、WSLのプロジェクトディレクトリで実行する。必要なものはGo、Docker Compose、Bash、Python 3（設定生成とHTTP検証用）。PostgreSQLとKeycloakはコンテナで動かす。

```bash
bash scripts/setup-local.sh
docker compose up -d --wait db keycloak
bash scripts/dev.sh run -buildvcs=false ./cmd/migrate up
bash scripts/dev.sh run -buildvcs=false ./cmd/web
```

[http://localhost:8080](http://localhost:8080) を開く。`ログイン` を押し、ユーザー名 `alice` または `bob` と、`.env` の `LOCAL_USER_PASSWORD` の値を入力する。必ず `localhost` で開く。

ログイン後は「本を追加」から書名・状態・メモを登録できる。他人の本は一覧に出ず、URLを直接指定しても操作できない。[操作・認可・ログの説明](docs/runbooks/reading-tasks.md)を参照。

アプリの停止は `Ctrl+C`。DBとKeycloakも止める場合は `docker compose stop`。データは残る。

初回のスクリプトはランダムなDBパスワードを含む `.env` を作る。既存の値を保持して足りない設定だけ追加し、Gitにも含めない。`scripts/dev.sh` はこのファイルを読み込んでGoを実行する。

Go 1.27.4は、この環境では `.tools/go` に配置済み。`scripts/go.sh` はこのSDKを優先し、なければPATH上のGoを使う。別環境では [.go-version](.go-version) に記載したGoを[公式手順](https://go.dev/doc/install)で用意する。SDKとキャッシュはGit管理対象外。

## 確認

```bash
# DBなしのテストと静的検証
bash scripts/go.sh test ./...
bash scripts/go.sh vet ./...

# DB起動後：実DBのマイグレーション・制約・セッション・所有者制限も検証
bash scripts/dev.sh test ./...

# アプリ起動後
curl -i http://localhost:8080/healthz
curl -i http://localhost:8080/readyz

# Keycloakのログイン画面を使う、2ユーザーのHTTP検証
python3 scripts/verify-login.py
```

`/healthz` はアプリの稼働確認。`/readyz` はDBへ接続できれば200、接続できなければ503を返す。Keycloakの状態は `docker compose ps` で確認する。テーブルが揃っているかはマイグレーションの `status` で確認する。

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
| `APP_BASE_URL` | ブラウザーから開くURL。初期値は `http://localhost:8080` |
| `KEYCLOAK_PORT` | Keycloakのホスト側ポート。初期値は8081 |
| `OIDC_ISSUER` | 信頼する認証サービスとrealmのURL |
| `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` | Goアプリが認証サービスへ接続する識別子・秘密情報 |
| `KEYCLOAK_ADMIN_PASSWORD` | ローカルKeycloak管理者 `admin` のパスワード |
| `LOCAL_USER_PASSWORD` | ローカル検証用の `alice` / `bob` 共通パスワード |

`dev.sh` は `.env` の値を読み込むので、待受ポートを変えるときは `.env` を編集する。DBポート変更時は両方の接続URLも揃える。初期化済みのDBのパスワードは、ファイルを編集するだけでは変更されない。[DBの操作・調査手順](docs/runbooks/local-database.md)を参照。

## ファイル配置

```text
cmd/web/                 アプリの起動・設定・正常終了
cmd/migrate/             テーブル変更の適用・状態確認
internal/web/            HTTPルート、ログ、画面、テスト
internal/database/       DB接続とマイグレーション、DBテスト
internal/auth/           OIDC、DBセッション、Cookie、認証テスト
internal/books/          読書タスクの項目、入力検証、所有者を条件にしたSQL
migrations/              テーブル定義のSQL
compose.yaml             ローカルのPostgreSQLとKeycloak
docker/postgres/         初回のDBユーザー・テスト用DB作成
docker/keycloak/         ローカル認証用realm・クライアント・2ユーザー
scripts/                 設定生成とGoの実行補助
docs/                    設計判断・手順・検証記録
TODO.md                  作業順と進捗
```

## 記録

- [フェーズ1の構成案・到達点](docs/phase1-design.md)
- [Go・OIDCの採用理由](docs/adr/0002-go-and-oidc.md)
- [開発環境とインストール対象](docs/runbooks/development-environment.md)
- [ログインの起動・設定・調査手順](docs/runbooks/local-auth.md)
- [セッションとログアウトの設計判断](docs/adr/0003-local-auth-and-sessions.md)
- [DBの操作・調査・削除手順](docs/runbooks/local-database.md)
- [ステップ1の検証結果](docs/verification/01-go-foundation.md)
- [ステップ2の検証結果](docs/verification/02-postgresql.md)
- [ステップ3の検証結果](docs/verification/03-oidc.md)
- [読書タスクの操作・認可・調査手順](docs/runbooks/reading-tasks.md)
- [ステップ4の検証結果](docs/verification/04-reading-tasks.md)

UIは不要なセカンダリラベルや説明を避け、レイアウトで伝える。
