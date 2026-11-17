# ADR-0004：DB初期化を単発コマンドへ分離する

日付：2026-11-17（日本時間）
状態：採用。ローカル検証済み、AWS実行は未実施。

## 決定

Goの `bootstrap initialize` をWeb・migrateと別のコンテナにする。公式AWS SDK for Go v2でRDS管理者Secretを読み、アプリ用Secretに接続URLを保存する。Terraformは保存先と権限だけを管理し、実値を受け取らない。初期化用のタスクロールだけが管理者Secretを取得できる。

初回はDBで同時実行ロックを取り、アプリ接続情報をSecretへ保存してから、そのパスワードで `readinglog` ユーザーを作る。保存成功後にDB処理が失敗しても、再実行で保存済みの値を使える。既存ユーザーのパスワードは変更しない。不一致やSecret紛失は停止して復旧を求める。管理者側の自動ローテーションとアプリ側のパスワード変更は別の作業。

Secretに値があることは初期化成功を意味しない。初期化の終了コード0と接続検証成功を確認し、その後migrate、Webの順で動かす。初期化は稼働中の認証情報を切り替える仕組みではない。

## 選択肢とトレードオフ

| 論点 | 採用 | 代替案・影響 |
| --- | --- | --- |
| 実行場所 | 将来はアプリ用プライベートサブネットの単発Fargateタスク | 手元のAWS CLI・psqlによる作業は簡単だが、DBへの接続経路と秘密値の受け渡しが増える |
| API呼び出し | 初期化専用Goコマンドと公式SDK | CLIを子プロセスで呼ぶ方法より、値を引数へ出さず、エラー処理とテストをGoにまとめやすい。SDK依存関係は増える |
| 保存順序 | 初回の接続情報を先にSecretへ保存 | DBを先に作ると、保存失敗時にパスワードを失う。先に保存する方式は、完了前のSecretでWebを起動しない手順が必要 |
| 再実行 | 同じ値の再利用・DBのロック・不一致時の停止 | 稼働中のパスワード変更には別のローテーション設計が必要。手動のSecret変更や外部処理との排他まで保証しない |
| DB権限 | 管理者がDB・publicスキーマを保持し、アプリへCONNECTとpublicのUSAGE/CREATEを付与 | スキーマ所有権を渡す当初案から絞る。作ったテーブルはアプリ所有なのでWebもDDLを実行できる。実行用・変更用ロールの分離は後続 |

DB・Secretをまたぐ単一トランザクションは使えない。DB内のロール作成と権限設定はトランザクションで行い、Secret側の書き込み結果が不明でも既存値を読み直して復帰する。SDK再試行には同じClientRequestTokenを使う。

## 根拠と検証

- [AWS：PutSecretValueと冪等なリクエスト](https://docs.aws.amazon.com/secretsmanager/latest/apireference/API_PutSecretValue.html)
- [AWS：GetSecretValueとAWSCURRENT](https://docs.aws.amazon.com/secretsmanager/latest/apireference/API_GetSecretValue.html)
- [PostgreSQL：GRANT](https://www.postgresql.org/docs/18/sql-grant.html)
- [PostgreSQL：advisory lock](https://www.postgresql.org/docs/18/functions-admin.html#FUNCTIONS-ADVISORY-LOCKS)
- [ローカル検証結果](../verification/11-database-bootstrap.md)
