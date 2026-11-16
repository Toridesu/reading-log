# ステップ6E：GoのDB接続TLSとRDS CAの同梱

実施日：2026-11-16（日本時間）

## 変更と確認範囲

DB接続で証明書・ホスト名の検証を標準で要求し、平文へのfallbackも拒否するよう変更した。ローカルComposeには明示的な例外を設定した。RDSの東京リージョン用CAを公開HTTPS配布から取得し、Web・migrateへ同梱した。[再実行と調査手順](../runbooks/database-tls.md)

既存Docker Desktopをユーザー別インストール先から起動した。新しいソフトやGo依存ライブラリは追加していない。`.env` にはローカル用 `DATABASE_REQUIRE_TLS=false` だけを不足設定として追加し、既存パスワードを保持した。

## CAとイメージ

- 東京用のRSA2048 G1、RSA4096 G1、ECC384 G1の3ルートCAを確認。
- OpenSSLでCA属性・有効期間・自己署名を検証。
- バンドルのSHA-256：`d1df75221341812a657d7c7b070ff61ce78824fbf05acf499f7e394f5ff58d50`。
- Dockerビルド中の `sha256sum -c` が成功。
- 両イメージからCAを取り出して同じハッシュであることを確認。UID/GID `65532:65532` から読める属性も確認。
- 検査のために作った一時コンテナは削除。

## テスト結果

| 検査 | 結果 |
| --- | --- |
| Go `test ./...` / `vet ./...` | 成功 |
| TLS設定の拒否テスト | disable/allow/prefer/require/verify-ca等を拒否。verify-fullを受け付け、ローカルの明示的例外は維持 |
| 初期設定と秘密情報 | TLS指定がない場合は必須。不正な設定値・接続失敗のエラーへパスワードを出さない |
| `verify-database-tls.py` | 使い捨てPostgreSQL18.6で6ケース成功 |
| 既存DBの `bash scripts/dev.sh test ./...` | マイグレーション・セッション・CRUD・所有者制限の統合テストも成功 |
| Compose構文 / Web・migrateビルド | 成功 |
| migrate `status` | 00001・00002が適用済み |
| Webヘルスチェック | healthy、`/healthz` と `/readyz` が200 |
| Alice・Bobのログイン検証 | ログイン、callbackの再利用拒否、CSRF、ログアウト、失効済みCookieの拒否が成功 |
| 終了後の状態 | Webは正常停止（exit code 0）。既存DB・Keycloakは起動を維持 |
| ファイル検査 | Python・Shell構文、文書内のローカルリンク、CAハッシュ、既存の秘密値が追跡対象に含まれないことを確認 |

TLSの実測：

```text
verified_connection: PostgreSQL confirmed TLSv1.3
wrong_hostname: PASS
untrusted_ca: PASS
missing_ca: PASS
plaintext_fallback: PASS
server_rejects_plaintext: PASS
```

検証用DBは `hostssl` だけを受ける設定にした。Go側で例外的に平文を許可して試したケースも、サーバー側で接続を拒否した。検査後に専用コンテナ・匿名ボリューム・一時鍵を削除し、専用コンテナと `.cache/tls-*` が残っていないことを確認した。

## 未実施事項

RDSの公開CAの同梱確認と、独自の一時CAによるローカルTLS検証を行った。実RDSの証明書チェーン・SG・IAM・ネットワークを通した接続確認は未実施。AWSリソースの作成やSecret値の投入はない。

次は専用DBユーザーの初期化と、アプリ接続情報をSecretへ投入する処理を用意する。
