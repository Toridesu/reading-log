# ステップ5：ローカルの障害調査と復元

実施日：2026-11-15（日本時間）

## 対象

Goアプリ・PostgreSQL 18.6・既存Keycloakを使うローカル構成。バックアップと復元にはDBコンテナ内の同じバージョンのpg_dump / pg_restoreを使用。新しいパッケージやAWSリソースは導入していない。

[操作・設計判断・削除手順](../runbooks/local-operations.md)に沿い、`scripts/verify-operations.py`で一連の演習を実行した。Goアプリの処理には変更なし。

## 障害調査

| 状態 | /healthz | /readyz | ログイン済みGET /books | POST /books |
| --- | --- | --- | --- | --- |
| DB稼働 | 200 | 200 | 200 | 検証用4冊を追加できた |
| DB停止 | 200 | 503 | 503 | 503、書き込みなし |
| DB再開 | — | 200 | 200 | 元データが変わっていないことを照合 |

DB停止中もGoのプロセスは動いていた。DB再開後は同じPID・同じログインCookieのまま本棚へ戻れた。アプリ再起動は不要だった。

DB停止時の `/readyz` の実際のログ：

```json
{"time": "2026-11-15T01:24:15.316688947+09:00", "level": "WARN", "msg": "database_unavailable", "request_id": "REORCRPPD2R2JB52E4PH3QKCUO"}
{"time": "2026-11-15T01:24:15.31670095+09:00", "level": "INFO", "msg": "http_request", "request_id": "REORCRPPD2R2JB52E4PH3QKCUO", "method": "GET", "path": "/readyz", "status": 503, "duration_ms": 0}
```

`X-Request-ID`からこの2行を対応付けた。本棚の503にも、対応する `session_unavailable` と `http_request` が記録された。ComposeのDB停止と併せて原因を切り分けた。

## バックアップと別DBへの復元

| 項目 | 実測 |
| --- | --- |
| users | 2行。全列が一致 |
| reading_tasks | 検証用4行。所有者・日本語/絵文字/改行入りメモ・状態・日時を含む全列が一致 |
| goose_db_version | 3行。初期履歴と適用済み2マイグレーションが一致 |
| 採番 | users・reading_tasks・gooseのlast_valueとis_calledが一致 |
| sessions | 元DBに2行、復元先は0行 |
| login_requests | 元DBに1行、復元先は0行 |
| バックアップサイズ | 11,119バイト |
| バックアップ所要時間 | 約0.31秒 |
| 別DB作成・復元・ANALYZE | 約1.08秒 |

これは少量の検証データでの実測で、AWSや大きなDBの復旧時間の保証ではない。

バックアップは `pg_dump --format=custom`。ファイルは600、出力ディレクトリは700。認証用2テーブルは定義だけを保存し、行を除外した。新規DBへ `--single-transaction` で復元し、件数だけでなく全行と採番の一致を確認した。

## 復元後の動作と拒否

- 元DBで発行したログインCookieは復元DBでは使えず、トップへ戻った。
- 既存KeycloakでAlice・Bobとして再ログインすると、各自の復元された本を参照できた。
- 他人の本のIDを指定した参照・更新は404。自分の本の追加・更新・削除は成功。
- 復元先での操作後も、元DBの全行・採番が復元操作前と同じであることを確認。
- 既存の復元DBへの再復元、元DB名への復元・削除を拒否。上書きされない。
- SHA-256が一致しない破損アーカイブを拒否し、復元先DBを作らない。
- 追加検証として、TOCが読めるがデータ部が途中で切れたアーカイブを用意。チェックサムをこの検証用ファイルに合わせて復元工程まで進めると失敗し、その呼び出しで新規作成したDBだけが削除された。
- パスワード・クライアントシークレット・セッションCookie・CSRF・メモ本文がログに出ていないことを照合。

```text
PASS: DB outage, failure logs, recovery without app restart
PASS: backup, restore, row/owner/content/sequence comparison and unsafe-target rejection
PASS: restored app login, authorization and CRUD; source DB unchanged
PASS: test books/sessions removed, restore DB dropped, app stopped, original DB running
PASS: restore-time truncation failure removes only newly created restore DB
```

## 後片付けと成果物

検証で作った本4冊、元DBの検証用セッション2件、認証途中の行1件、復元DBを削除した。既存ユーザーは残し、元DBとKeycloakはhealthy、検証用Goプロセスは停止。採番値が進むことは許容する。

生ログ・結果JSONは `.cache/verification/05-operations-148d635cd968/`、バックアップは `.cache/backups/` にローカル保存した。個人データを含み得るこれらのファイルはGitへ入れない。この文書のログ抜粋はrequest_idと動作情報のみ。

## 残る範囲

この演習は同じPostgreSQLインスタンス内の別DBへ戻す検証。PC全体の故障からの復旧、Keycloakのデータ復旧、RDSの自動バックアップ・スナップショット・時点復元、AWS上のアプリ切替は未実施。次はTODOのステップ6のコンテナ・Terraform定義へ進む。
