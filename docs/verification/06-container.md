# ステップ6A：Webとマイグレーションのコンテナ化

実施日：2026-11-15（日本時間）

## 今回の範囲

Goをコンテナで実行する最小構成を作った。構成・採用理由・起動/停止/再作成の手順は[コンテナ運用手順](../runbooks/local-container.md)に記録。Terraform・ECRへのpush・AWSリソース作成はまだ行っていない。

Docker Desktopが停止していたため既存のDesktopを起動し、DBとKeycloakを既存ボリュームから再開した。OSパッケージのインストールは行わず、ビルド用GoイメージをDockerへ取得した。

## ビルドとイメージ

- Go公式 `1.27.4-bookworm` のタグとmanifest index digestを確認して固定した。
- 固定digest：`sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea`
- `docker compose --profile app config --quiet`、`build web migrate` が成功。
- ビルドコンテキストは約113kB。許可したソース・依存定義だけを送った。
- 実行用はscratch、linux/amd64、UID/GID `65532:65532`。

| イメージ | ローカルinspectのSize | アプリとして含めたファイル |
| --- | --- | --- |
| `readinglog:local` | 29,329,983 bytes（約29.3MB） | `/app/web`、`/app/healthcheck`、CA証明書 |
| `readinglog-migrate:local` | 15,158,089 bytes（約15.2MB） | `/app/migrate`、CA証明書 |

一時コンテナを作成してファイルシステムをexportし、上記とDocker管理ファイルだけであることを確認した。イメージ設定と各ファイルにローカル `.env` の実パスワード・クライアントシークレットが含まれないことを確認。検査用コンテナは削除した。

## 実行時設定

起動したコンテナのinspectで次を確認した。設定全体は秘密情報を含むため記録へ掲載しない。

- 非rootの `65532:65532` で実行。
- root filesystemは読み取り専用、capabilitiesはALL削除、no-new-privilegesが有効。
- 公開は `127.0.0.1:8080 → コンテナ8000` のみ。
- `healthcheck` が成功し、状態はhealthy。
- マイグレーション用コンテナも非root・読み取り専用で実行し、`applied 0 migration(s)`。既存の適用済みDBを使用。

## 認証とアプリの検証

| 検証 | 結果 |
| --- | --- |
| `bash scripts/go.sh test ./...` / `vet ./...` | 成功 |
| `bash scripts/dev.sh test ./...` | 実DBを使う認証・CRUD・マイグレーションのテストも成功 |
| 新しいOIDC接続補助のテスト | TCP接続先だけを変えてもログイン成功。バックエンドのURLをissuerとするトークン、不正署名・nonceを拒否 |
| 設定の拒否テスト | HTTPS環境でのローカル接続補助、不正ポート、許可しない接続先を拒否 |
| Host/接続先のテスト | 元のHTTP Hostを保持し、別のホスト・ポートへの通信は置換しない |
| `python3 scripts/verify-login.py` | 実KeycloakのAlice・Bobでログイン、再送/CSRF拒否、ログアウト、失効Cookie拒否が成功 |
| Chromiumの `scripts/verify-books.py` | コンテナ経由のCRUD、所有者制限、改ざん・CSRF拒否、PC/スマートフォン幅の画面が成功 |

ブラウザーに見せるissuerは `http://localhost:8081/realms/reading-log` のまま。Goからのdiscovery・トークン交換・公開鍵取得は `keycloak:8080` へTCP接続する。issuer不一致の許容や署名検証の省略は加えていない。

```text
alice: login, callback replay rejection, CSRF rejection, logout, revoked-cookie rejection OK
bob: login, callback replay rejection, CSRF rejection, logout, revoked-cookie rejection OK
PASS: real sessions, CRUD, ownership checks, tampering, CSRF, validation, mobile layout
PASS: deletion requires confirmation POST; deleted book returns 404
PASS: test books removed and sessions logged out
```

## 正常終了と再作成

1. Aliceでログインし、検証用の本を1冊追加。
2. `docker compose --profile app stop web` でSIGTERMを送信。
3. `server_stopping` → `server_stopped` と終了コード0を確認。
4. `up -d --force-recreate --wait web` で新しいコンテナIDになったことを確認。
5. 同じCookieで先ほどの本を開け、書名・メモも残っていた。
6. 検証用の本を削除し、ログアウト。

```text
PASS: SIGTERM -> graceful exit 0; recreated web has new container ID; existing session and book survive
PASS: verification book removed and session logged out
```

Webの標準出力からJSONログを取得でき、パスワード・クライアントシークレット・Cookie・CSRF・メモ本文が出ていないことも照合した。

## 記録と次の作業

ローカルの `.cache/verification/06-container/` に、必要項目だけ抜き出したイメージ検査結果、再作成結果、Webログを保存。生ログ・実行時設定・バックアップはGitへ入れない。ブラウザー検証で作った本も削除済み。最後にWebコンテナを終了コード0で停止し、DBとKeycloakは起動したまま残した。

今回はローカルのコンテナ実行まで。ARM64、RDSのTLS/証明書、Cognito、ALB、ECS上の稼働・停止・ヘルスチェック、コンテナの脆弱性検査は後続で確認する。次はTerraformの導入とAWS用定義へ進む。
