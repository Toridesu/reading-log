# ステップ6C：ECR・ログ・ECSクラスターとIAM

実施日：2026-11-16（日本時間）

## 今回の範囲

Web/migrateのプライベートECR・ロググループ・実行ロール・タスクロールと、Fargate用ECSクラスターをTerraformで定義した。[構成図・採用理由・再実行/削除の扱い](../runbooks/ecs-foundation.md)を参照。

Terraform 1.17.2、AWSプロバイダー6.73.0を継続使用。追加のツール導入やアプリ変更はない。AWSリソース作成、ECRへのpush、実AWSに対するplan/applyは行っていない。

## 検査結果

`bash scripts/check-terraform.sh` で次を確認した。

| 検査 | 結果 |
| --- | --- |
| `fmt -check -recursive infra` | 成功 |
| `init -backend=false -lockfile=readonly` | 成功。固定プロバイダーを再利用 |
| `validate` | `Success! The configuration is valid.` |
| `test` | `Success! 12 passed, 0 failed.` |

| 追加したrun | 確認内容 |
| --- | --- |
| `artifact_and_log_storage` | Web/migrateの保存先分離、タグ上書き禁止、強制削除無効、AES256、push時検査、ログ14日保持、削除方針、Fargateの指定 |
| `execution_permissions` | 実行ロールへの付与、ECR認証以外の対象ARN限定、pullのAction限定、自分のログストリームだけへの送信 |
| `ecs_role_trust` | 4つのロールの名前分離、ECSタスクだけの信頼先、アカウントと東京の条件 |
| `trust_follows_deployment_account` | 架空の展開先アカウントを変えると全ロールの信頼条件にも反映 |

既存のネットワーク8件も成功。ECRとログのmock ARNはWeb/migrateで異なる値とし、対象の取り違えを検出する。アカウントID取得のdata sourceもmockを使う。

`Resource = "*"` のECR認証APIと、対象リポジトリに限定するpull APIを別Statementにした。ログは対象ロググループの `:log-stream:*` に限定。公式のAction/Resource仕様と、[固定プロバイダーがロググループARNの末尾ワイルドカードを除去する実装](https://github.com/hashicorp/terraform-provider-aws/blob/v6.73.0/internal/service/logs/group.go)を確認した。タスクロールへの許可ポリシーは定義していない。

文書のローカルリンクと差分の空白チェックも成功。SDK・キャッシュ・state・実値のtfvars・`.env` はGitから除外され、検査後にstateファイルがないことを確認した。ローカルの実パスワード・クライアントシークレットがGitへ入る候補ファイルに含まれないことも照合した。

## 未検証と次の作業

テストは定義から生成する値を調べたもので、AWS上で権限拒否やネットワーク通信を起こした結果ではない。実アカウントのSCP・既存ポリシー・Registry設定・ログ保持ポリシー、ECRのpush/pull・スキャン結果・ログ送信は実展開時に確認する。実環境のIAMシミュレーターやIaCスキャナーも未実施。

ALB、RDS、Cognito、Secret本体・取得権限、ECSタスク定義/サービス、state基盤は後続。次はRDSと秘密情報の管理へ進む。
