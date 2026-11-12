# 開発環境とインストール対象

確認日：2026-11-11（日本時間）。このワークスペースから利用するWSL2 / Linux x86_64環境についての確認。Windows側のすべてのインストール状況を調査した結果ではない。

ステップ1でGo 1.27.4をプロジェクト内の `.tools/go` へ配置済み。システム全体のPATHは変更せず、`bash scripts/go.sh` から実行する。公式配布一覧のSHA256とダウンロードしたSDKの一致を確認した。

## 現在確認できたもの

| 対象 | 確認結果 | 今後の扱い |
| --- | --- | --- |
| Git | 2.53.0。mainブランチで初期化・初回コミット済み | mainをorigin/mainへpush済み。作業単位でコミット・pushする |
| Docker CLI / Engine | 29.9.2。サーバーバージョンの取得成功 | そのまま使用。イメージ取得・ビルド・コンテナ起動は別途確認 |
| Docker Compose | v5.6.1 | そのまま使用 |
| Go | `.tools/go` に1.27.4を配置済み | `bash scripts/go.sh` で使用 |
| Terraform | このWSLのPATH上では見つからない | インフラ定義の検証段階で用意 |
| AWS CLI | このWSLのPATH上では見つからない | AWSへ展開する段階で用意 |
| Node.js / Make | このWSLのPATH上では見つからない | 初期構成では必須にしない |

Dockerの最初の確認は実行サンドボックス内のソケット権限制限で失敗したが、許可された読み取り確認ではサーバーバージョンを取得できた。ユーザーのDocker設定が壊れているとは判断しない。ソケットの権限を緩める変更は不要。

## 必要になるもの

開発コマンドはこのWSL環境から実行する。Windows版とWSL版のツールを混ぜず、必要な段階で追加する。SDKやCLI本体はGitへ入れず、バージョン指定と設定・コードを管理する。

Gitは個人開発に合わせ、まずmainブランチで作業単位のコミットを残す。大きな変更は必要に応じて短いブランチを使う。初期から複雑なブランチ運用やレビュー必須ルールを設けない。利用者が作成した公開リポジトリ [Toridesu/reading-log](https://github.com/Toridesu/reading-log) をoriginに設定済み。利用者の承認を受けて初回pushを完了し、mainはorigin/mainを追跡している。

### アプリ開発

- Go SDK：ビルド、`go test`、`go vet`、`gofmt`に使用する。Go Modulesで依存ライブラリを取得し、`go.mod` / `go.sum`を管理する。
- Docker / Compose：PostgreSQL、Keycloak、アプリのコンテナを起動する。
- Git：コード、設計判断、検証記録を管理する。

2026-11-11にGo公式配布一覧で1.27.4を確認し、Linux amd64版をダウンロードしてチェックサムを照合した。バージョンは `.go-version` に記録。SDKとキャッシュは `.gitignore` で除外している。別環境ではこのバージョンのGoを公式手順で用意すれば、同じ実行補助スクリプトを使用できる。

PostgreSQLとKeycloakはComposeがイメージを取得して動かす。ホストへPostgreSQLやJavaを別途インストールしない。最初の起動時にはイメージやGo依存パッケージのダウンロードが発生する。

Keycloakはローカル開発用のモードを使い、公開ポートをループバックに限定する。開発用の認証情報は本番へ持ち込まない。Goアプリとブラウザーの双方から同じOIDC issuerを参照できるよう、Composeのホスト名・ポート・redirect URIを合わせて設計する。

### 後で用意するもの

- Terraform：ステップ6の `fmt`、`validate`、IaC検査を行う段階に、公式配布のLinux版を導入する。Goと同様にプロジェクト内 `.tools/` に配置する案。CLIバージョンと `required_version` を揃え、プロバイダーは `.terraform.lock.hcl` で固定する。stateはGitへ入れない。
- セキュリティ検査ツール：フェーズ2で選定し、可能なものはバージョンを固定したコンテナやGoツールとして実行する。
- AWS CLI：実際のAWS展開・調査を始めるステップ9で、公式のLinux用CLI v2を導入する。認証設定は実展開時に用意し、コードやGitへ認証情報を保存しない。GitHub Actions側のAWS認証はOIDCとして別に設定する。
- エディターのGo補完：必要に応じてgopls等を導入する。アプリを起動するための必須条件ではない。

## 初期セットアップの完了条件

1. Goのバージョンを取得できる、または指定した開発コンテナ内でGoを実行できる。
2. Composeの設定が検証できる。
3. PostgreSQLとKeycloakをローカルで起動し、ヘルスチェックが通る。
4. GoアプリがDBとOIDC issuerへ接続できる。
5. ブラウザーからログインしてコールバックへ戻れる。

Goの起動・テスト・ビルドとHTTPサーバーの応答・正常終了まで確認済み。Compose、PostgreSQL、Keycloakを組み合わせた起動確認は後続のステップで行う。[ステップ1の結果](../verification/01-go-foundation.md)

## 公式資料

- [Goのインストール](https://go.dev/doc/install)
- [Goのリリース履歴](https://go.dev/doc/devel/release)
- [Keycloak Dockerクイックスタート](https://www.keycloak.org/getting-started/getting-started-docker)
- [Docker DesktopとWSL](https://docs.docker.com/desktop/features/wsl/)
