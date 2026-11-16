# ステップ6B：Terraformの導入とネットワーク定義

実施日：2026-11-16（日本時間）

## 今回の範囲

Terraformをプロジェクト内へ導入し、東京2AZのVPC・公開/アプリ/DB用の6サブネット・ルート・NAT・S3 Gateway Endpoint・SGを定義した。[構成図・採用理由・再実行手順](../runbooks/terraform-network.md)を参照。

AWS CLIやAWS認証の設定、実AWSに対するplan/apply、リソース作成は行っていない。ALB・ECS・RDS本体とIAM、state保存先は後続の作業。

## ツールと検査結果

| 対象 | 結果 |
| --- | --- |
| Terraform CLI | 公式Linux amd64版1.17.2を `.tools/terraform` に配置。ZIPのSHA-256、公式一覧、リポジトリの固定値が一致 |
| AWSプロバイダー | 6.73.0。初期化に成功し、ロックファイルを生成。init出力でHashiCorpの署名を確認 |
| `fmt -check -recursive infra` | 成功 |
| `init -backend=false -lockfile=readonly` | 固定バージョンの再利用に成功 |
| `validate` | `Success! The configuration is valid.` |
| `test` | `Success! 8 passed, 0 failed.` |

CLI ZIPのSHA-256：

```text
e6f16b2f0b137ee5729caa8c4e79a3bae4ee261acd198a1fcfa8584a6296605b
```

検査は `bash scripts/check-terraform.sh` で実行。AWS環境変数・共有設定・認証ファイルの参照を外し、AWSプロバイダーはmockへ置き換えた。`init` の配布物取得にはインターネットを使用する。

## mockテストの内容

| run名 | 確認したこと |
| --- | --- |
| `network_paths` | 3層×2AZ、重複しない6つの/24、プライベート側の自動IP付与無効、公開→IGW、アプリ→1つのNAT、各サブネットとルート表の対応、DBに追加ルートなし |
| `security_boundaries` | ALBの443、ALB→アプリ8000、アプリ→DB5432、アプリの外向き443、SG参照先の対応、デフォルトSGのルールなし |
| `s3_image_layers_only` | S3 Gatewayはアプリ用ルート表だけに関連付け、東京ECRレイヤーバケットのGetObjectだけを許可 |
| `alternate_private_cidr` | VPCを10.30.0.0/16へ変えると全サブネットへ反映 |
| `reject_public_cidr` | 許可する10.x.0.0/16形式以外を拒否 |
| `reject_duplicate_az` | 同じAZの重複指定を拒否 |
| `reject_other_region_az` | 東京以外のAZ指定を拒否 |
| `reject_single_az` | AZが1つだけの指定を拒否 |

SGの取り違えを検出できるよう、mock内でALB・アプリ・DBへ別々のSG IDを割り当てた。

## ファイルの確認

新しいシェルスクリプトと埋め込みPythonの構文、文書のローカルリンクを確認した。SDK・キャッシュ・実値のtfvars・stateはGitから除外され、依存ロックと設定例は管理対象になっている。ローカルの実パスワード・クライアントシークレットがGitへ入る候補ファイルに含まれないこと、検査後にstateファイルが生成されていないことも確認した。

## 検証の限界と次の作業

これは定義の検査であり、AWS上で通信を試した結果ではない。IAM・クォータ・AZの利用可否・実際のルートやSG動作・イメージ取得は実展開時に確認する。IaCセキュリティスキャナーの導入も未実施。アプリコードは今回変更していない。

次は、このネットワークを参照するECR・アプリ実行基盤などの定義を小さな単位で追加する。ネットワークだけが完成した段階として、TODOのステップ6全体は未完了のままにする。
