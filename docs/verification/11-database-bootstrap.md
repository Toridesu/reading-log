# ステップ6F：DB初期化とSecret値の投入処理

実施日：2026-11-17（日本時間）

## 範囲

Goの初期化コマンド、Secrets Manager取得/保存処理、専用Dockerターゲット、ECR・ログ・IAMを追加した。AWS SDK for Go v2をGoモジュールとして取得し、`go.mod` / `go.sum` に固定。AWS CLIの導入やAWS認証設定は行っていない。[設計判断](../adr/0004-database-bootstrap.md)・[実行/復旧手順](../runbooks/database-bootstrap.md)

既存Docker Desktopを起動し、Composeとは別の使い捨てPostgreSQL 18.6を使用。管理者はスーパーユーザーではない `readinglog_admin` を作って使用し、一時CAによるTLS・ホスト名検証を有効にした。Secret操作はメモリ上の代替APIへ置き換え、AWSへの送信はない。

## 検証結果

| 検査 | 結果 |
| --- | --- |
| Go `test ./...` / `vet ./...` | 成功。外部DB未指定の既存統合テストはskip |
| URL/SDK境界の単体テスト | 特殊文字のURL往復、別ホスト・ユーザー・TLS変更・追加設定の拒否、AWSCURRENT/リクエストトークン、不正な値・生エラーの秘匿を確認 |
| `python3 scripts/verify-database-tls.py --bootstrap` | 下記12ケース成功 |
| 既存のTLS検査 | 6ケース成功。実測TLSv1.3 |
| Terraform fmt/init/validate/test | AWS認証なしのmock検証で19件成功 |
| Dockerビルド | bootstrap・Web・migrateに成功。最終bootstrapコードでも再ビルド成功 |
| イメージ検査 | 非root `65532:65532`、RDS CAのSHA-256一致を確認 |
| 引数なし起動 | ネットワークなし・読み取り専用で起動し、usageと終了コード1。認証・AWS呼び出し前に停止 |
| 片付け | 専用コンテナ・匿名ボリューム・一時鍵を削除。検査用コンテナも削除 |

## DB初期化の12ケース

1. Secret保存失敗時はDBユーザーを作らない。
2. 保存成功後に応答を失っても、値が残り再利用できる。
3. DBユーザー作成権限が不足して失敗しても、Secretの値を保持する。
4. ユーザー作成後にスキーマ権限設定が失敗すると、作成したユーザーもロールバックする。
5. 同じSecretで初期化・再実行が成功し、Secretバージョンを増やさない。
6. アプリユーザーで2件のマイグレーションを適用・再適用し、ユーザーと読書タスクを保存できる。
7. DB作成・ロール作成・新しいスキーマ作成はSQLSTATE 42501で拒否される。
8. パスワード不一致時は停止し、元のパスワードを上書きしない。
9. ユーザーだけ存在してSecretがない場合は停止する。
10. 既存アプリユーザーに管理権限が追加されている場合は停止する。
11. 別の初期化処理がロックを保持している間は停止する。
12. 再初期化後も登録した読書タスクが残り、Secret書き込み回数が増えない。

## 未検証・後続

実AWSリソースの作成・plan/apply・Secretへの投入・ECR push・ECS起動は行っていない。実RDSの管理者権限・実CAチェーン・IAM評価・CloudTrail/CloudWatchの実ログは後日の実機検証で確認する。タスク定義への接続、初期化成功を条件にしたデプロイ順序の実装、パスワード変更・復元先の接続情報変更は後続。

次はCognitoとOIDCクライアント用Secretを定義する。
