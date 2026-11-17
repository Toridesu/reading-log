# 読書管理アプリ

AWSの設計・構築・運用、CI/CD、脅威検知・対応を学ぶための個人開発。

リポジトリ：[Toridesu/reading-log](https://github.com/Toridesu/reading-log)。進捗は [TODO.md](TODO.md) で管理する。

現在はステップ5まで完了。Keycloak経由のログイン・ログアウトと、自分の読書タスクの作成・一覧・編集・削除が動く。DB停止からの復旧と、バックアップを別DBへ戻して操作する検証も完了した。ステップ6はコンテナ化、Terraformのネットワーク・ECR・ログ・ECSクラスター・IAM・RDS・DB用Secretの定義とmockテストまで完了。Go側のTLS検証とRDS CAの同梱も完了。DB初期化・Secret値の投入処理もローカルで検証済み。次はCognitoとOIDC用Secretを定義する。当面はAWSリソースを作成しない。

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

## コンテナで起動する

WSLで動かしているGoを止めてから実行する。同じDBとKeycloakを使用する。

```bash
docker compose up -d --wait db keycloak
docker compose --profile app build web migrate
docker compose --profile app run --rm migrate up
docker compose --profile app up -d --wait web
```

URLは同じ [localhost:8080](http://localhost:8080)。ログは `docker compose --profile app logs --tail 50 web`、停止は `docker compose --profile app stop web`。[構成・認証の通信経路・再作成手順](docs/runbooks/local-container.md)を参照。

## Terraformのローカル検査

AWS用定義は `infra/dev/`。Linux x86_64 / WSLで次を実行する。AWS CLIやAWS認証情報は不要。初回は公式サイトからCLIとプロバイダーを取得する。

```bash
bash scripts/setup-terraform.sh  # 初回のみ
bash scripts/check-terraform.sh
```

Terraform 1.17.2は `.tools/terraform`、AWSプロバイダーは6.73.0で固定。検査は書式・構文・18件のmockテストで、AWSリソースを作らない。[ネットワーク手順](docs/runbooks/terraform-network.md)と[ECR・ログ・IAMの説明](docs/runbooks/ecs-foundation.md)、[RDS・Secret・復元の手順](docs/runbooks/rds-and-secrets.md)を参照。

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

# アプリの起動は不要：専用DBでTLSを検証して片付ける
python3 scripts/verify-database-tls.py

# 初期化・再実行・権限拒否を、使い捨てDBと代替APIで検証
python3 scripts/verify-database-tls.py --bootstrap
```

`/healthz` はアプリの稼働確認。`/readyz` はDBへ接続できれば200、接続できなければ503を返す。Keycloakの状態は `docker compose ps` で確認する。テーブルが揃っているかはマイグレーションの `status` で確認する。

アプリの標準出力はJSONログ。レスポンスの `X-Request-ID` とログの `request_id` を照合し、`path`、`status`、`duration_ms` を見る。DB障害時には `database_unavailable` を記録する。クエリ文字列、Authorization、Cookie、リクエスト本文は記録しない。

## バックアップと復元

GoアプリとWebコンテナを停止し、DBとKeycloakを起動した状態で、一連の運用演習を再実行できる。DBを一時停止・再開し、復元用DBは最後に削除する。

```bash
bash scripts/go.sh build -o bin/readinglog ./cmd/web
python3 scripts/verify-operations.py
```

個別のバックアップ・復元・照合は `python3 scripts/local-db.py --help` を参照。[障害調査・復元・削除の手順](docs/runbooks/local-operations.md)に説明をまとめた。

## 設定

設定項目は [.env.example](.env.example)、実際の値はローカルの `.env` に置く。

| 環境変数 | 用途 |
| --- | --- |
| `APP_ADDR` | アプリの待受先。初期値は `127.0.0.1:8080` |
| `DB_PORT` | ホスト側のDBポート。初期値は5432 |
| `POSTGRES_PASSWORD` | DB初期化用の管理者パスワード |
| `APP_DB_PASSWORD` | アプリ専用DBロールのパスワード |
| `DATABASE_URL` | アプリとマイグレーションの接続先 |
| `DATABASE_REQUIRE_TLS` | 未設定時は証明書・ホスト名検証を必須にする。ローカルの `.env` / Composeだけfalse |
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
cmd/healthcheck/         コンテナ内でGoの稼働を確認するコマンド
internal/web/            HTTPルート、ログ、画面、テスト
internal/bootstrap/      AWS向けDB初期化・Secret取得/保存・再実行の検証
internal/database/       DB接続とマイグレーション、DBテスト
internal/auth/           OIDC、DBセッション、Cookie、認証テスト
internal/books/          読書タスクの項目、入力検証、所有者を条件にしたSQL
migrations/              テーブル定義のSQL
certs/                   東京RDSの公開CA・固定チェックサム
Dockerfile               Web・DB変更・初期化用イメージのビルド
.dockerignore            ビルドへ渡すファイルの制限
compose.yaml             DB・Keycloak、appプロファイルのWebとmigrate
docker/postgres/         初回のDBユーザー・テスト用DB作成
docker/keycloak/         ローカル認証用realm・クライアント・2ユーザー
infra/dev/               AWSネットワーク・ECR・ECS・ログ・IAM・RDS・Secret、mockテスト
scripts/                 設定生成、Go/Terraform実行補助、検証、DBバックアップ・復元
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
- [障害調査・バックアップ・復元手順](docs/runbooks/local-operations.md)
- [ステップ5の検証結果](docs/verification/05-local-operations.md)
- [コンテナの起動・通信・調査手順](docs/runbooks/local-container.md)
- [ステップ6Aの検証結果](docs/verification/06-container.md)
- [Terraformの構成図・通信経路・検査手順](docs/runbooks/terraform-network.md)
- [ステップ6Bの検証結果](docs/verification/07-terraform-network.md)
- [ECR・ログ・ECS・IAMの構成と確認手順](docs/runbooks/ecs-foundation.md)
- [ステップ6Cの検証結果](docs/verification/08-ecs-foundation.md)
- [RDS・秘密情報・バックアップ/削除の設計と手順](docs/runbooks/rds-and-secrets.md)
- [ステップ6Dの検証結果](docs/verification/09-rds-and-secrets.md)
- [DB接続のTLS・CA・拒否テストの手順](docs/runbooks/database-tls.md)
- [ステップ6Eの検証結果](docs/verification/10-database-tls.md)
- [DB初期化の設計判断](docs/adr/0004-database-bootstrap.md)
- [DB初期化・再実行・障害時の手順](docs/runbooks/database-bootstrap.md)
- [ステップ6Fの検証結果](docs/verification/11-database-bootstrap.md)

UIは不要なセカンダリラベルや説明を避け、レイアウトで伝える。
