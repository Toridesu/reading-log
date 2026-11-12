# ステップ2：PostgreSQLとマイグレーション

実施日：2026-11-12（日本時間）。WSL上のGo 1.27.4、Docker Desktop / Engine 29.9.2、Compose v5.6.1、PostgreSQL 18.6でローカル検証した。

## 結果

| 確認 | 実測結果 |
| --- | --- |
| Composeの設定と起動 | `config --quiet` 成功。DBはhealthy、ホスト側は127.0.0.1:5432のみ |
| Goテスト・vet・ビルド | `scripts/go.sh test ./...`、`vet ./...`、`build -o bin/readinglog ./cmd/web` 成功 |
| 初回マイグレーション | statusはpending、upで1件適用、statusはapplied |
| 再適用 | upは0件。既存テーブルやデータを作り直さない |
| 専用DBでの統合テスト | `scripts/dev.sh test -v ./internal/database` 成功。初回適用・再適用、保存、制約、関連行の削除を確認 |
| アプリ用DB権限 | current_userはreadinglog。rolsuper / rolcreatedb / rolcreateroleはすべてfalse |
| 正常時HTTP | `/healthz`、`/readyz` ともに200、statusはok |
| DB停止時HTTP | `/readyz` は503、statusはunavailable。`/healthz` は200のまま |
| DB停止時ログ | database_unavailableと503のhttp_requestを同じrequest_idで追跡できた。接続URL・パスワードは出力しない |
| コンテナ再作成と永続化 | 検証用ユーザーとタスクを保存し、`up -d --force-recreate --wait db` 後も同じID・書名・状態を取得できた |
| DB復旧 | アプリ再起動なしでreadyzが200へ復帰。マイグレーション再適用は0件 |
| 後片付け | 作成した検証用ユーザーを削除し、関連タスクが0件になった。アプリは停止ログを出して終了。DBは起動状態で残した |

統合テストではissuerとsubjectの組の重複、存在しない所有者、空白の書名、未知の状態、短すぎるセッションハッシュ、作成時刻より前の期限をDBが拒否することを確認した。同じsubjectでもissuerが違えば別ユーザーになる。

HTTPテストではDB障害を再現し、DBエラーに含めた疑似秘密情報がレスポンスとアプリログへ出ないことも確認した。

## 範囲と残り

- ログイン・セッションの有効性確認・タスクCRUD・所有者による認可は未実装。テーブルがあることだけではこれらの保護は成立しない。
- ボリュームによる永続化を確認した。バックアップからの復元はステップ5で行う。
- AWSリソースは作成していない。RDS上のバージョン・TLS・権限・バックアップは別途確認する。

再実行の手順は[ローカルPostgreSQL](../runbooks/local-database.md)を参照。
