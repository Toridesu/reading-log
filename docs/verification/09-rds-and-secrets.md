# ステップ6D：RDS・Secrets Managerの定義

実施日：2026-11-16（日本時間）

## 今回の範囲

プライベートRDS PostgreSQL、TLS必須のパラメーター、暗号化ストレージ、7日バックアップ、最終スナップショット、DBログ、アプリ用Secretと取得権限を定義した。[設計理由・接続準備・復元/削除の手順](../runbooks/rds-and-secrets.md)を参照。

Terraform 1.17.2 / AWSプロバイダー6.73.0を継続使用。AWSへリソースを作らず、Secret値も生成・投入・取得していない。アプリコード・Dockerfile・既存ローカルDBは変更していない。

## 実行結果

`bash scripts/check-terraform.sh` が成功した。

| 検査 | 結果 |
| --- | --- |
| fmt | 成功 |
| init（backend無効・lockfile読み取りのみ） | 成功。固定プロバイダーを再利用 |
| validate | `Success! The configuration is valid.` |
| mock test | `Success! 18 passed, 0 failed.` |

| 追加したrun | 確認したこと |
| --- | --- |
| `private_database` | DB用サブネット・SGへの対応、公開無効、Single-AZ、暗号化・容量、版、TLS設定とCA、DBログ保持・SQL本文の記録設定 |
| `backup_and_delete_defaults` | 7日バックアップ、削除保護、最終スナップショット、自動バックアップ保持、保守/バックアップ時間と更新方針 |
| `application_secret_boundary` | RDS管理者パスワードの管理委譲、明示パスワードなし、アプリ用Secretだけを各実行ロールに許可 |
| `prepare_intentional_delete` | 削除保護を解除しても最終スナップショット必須を維持し、指定名を使う |
| `reject_invalid_snapshot_name` | 連続ハイフンを含むスナップショット名を拒否 |
| `reject_invalid_database_prefix` | RDSの名前として不適切な末尾ハイフンを含む接頭辞を拒否 |

従来のネットワーク8件・ECS基盤4件も成功。各テストでRDS管理者SecretのARNをmockへ差し替えた。実行ロールが取得できるアプリ用Secretには別の架空ARNを使い、管理者用と取り違えないことを検査した。

文書のローカルリンクと差分の空白チェックも成功。ローカルの実パスワード・クライアントシークレットがGitへ入る候補ファイルに含まれないこと、SDK・キャッシュ・state・実値のtfvars・`.env` が除外されていることを確認した。検査後にstateファイルは生成されていない。

## 設定レビューと未実施事項

RDSに `password` / `password_wo` を渡す定義、Secretのバージョン作成、Secretの実値を取得するdata source、パスワードのoutputは追加していない。管理者パスワードはAWS側の生成に委ね、アプリ接続URLの値はTerraformの外で扱う設計。

AWS上の18.6・db.t4g.microの利用可否、パラメーター適用、TLS接続、Secret取得可否、CloudWatch送信、スナップショット/PITRからの復元は未検証。mockでIAMの実評価やサービス動作を再現したとは扱わない。

DB専用ユーザー初期化、RDS CAのイメージ同梱とGo側の検証、Secret値投入、ECSへの注入は次の実装。その後Cognito・ALB・ECSサービスへ接続する。ステップ6全体は引き続き未完了。
