# 読書管理アプリ

AWSの設計・構築・運用、CI/CD、脅威検知・対応を学ぶための個人開発。

リポジトリ：[Toridesu/reading-log](https://github.com/Toridesu/reading-log)

進捗は [TODO.md](TODO.md) で管理する。個人開発として、小さく動く単位で作っていく。

現在はステップ1「Goアプリの骨組み」まで実装・確認済み。トップ画面、ヘルスチェック、JSONログが動く。DB、ログイン、読書タスクの操作はこれから追加する。

構成はGo + PostgreSQL。認証はOpenID Connect（Authorization Code + PKCE）を使い、ローカルではKeycloak、AWS展開時はCognitoに接続する予定。当面はAWSリソースを作成しない。

## 起動

WSLのこのプロジェクトディレクトリで実行する。

```bash
bash scripts/go.sh run -buildvcs=false ./cmd/web
```

[http://localhost:8080](http://localhost:8080) を開く。停止は `Ctrl+C`。

Go 1.27.4は、この作業環境では `.tools/go` に配置済み。`scripts/go.sh` はこのSDKを優先し、なければPATH上のGoを使う。別の環境では [.go-version](.go-version) に記載したGoを[公式手順](https://go.dev/doc/install)で用意する。キャッシュとSDKはGitの管理対象外。

`-buildvcs=false` はビルド時のGit情報の埋め込みを無効にする指定。ソースコードはmainブランチでGit管理している。

## 確認

```bash
bash scripts/go.sh test ./...
bash scripts/go.sh vet ./...
curl -i http://localhost:8080/healthz
```

`/healthz` は `200` と `{"status":"ok"}` を返す。現段階ではHTTPサーバーの稼働確認であり、DB等の接続確認は含まない。

リクエストごとに標準出力へJSONログを出す。レスポンスの `X-Request-ID` とログの `request_id` を照合し、`path`、`status`、`duration_ms` を確認する。クエリ文字列、Authorization、Cookie、リクエスト本文はログへ出さない。

## 設定

| 環境変数 | 初期値 | 用途 |
| --- | --- | --- |
| `APP_ADDR` | `127.0.0.1:8080` | HTTPの待受先 |

ポートを変える例：

```bash
APP_ADDR=127.0.0.1:8081 bash scripts/go.sh run -buildvcs=false ./cmd/web
```

[.env.example](.env.example) は設定項目の見本。現段階では `.env` を自動読み込みしない。

## ファイル配置

```text
cmd/web/                 起動・設定・正常終了
internal/web/            HTTPルート、ログ、画面、テスト
scripts/go.sh            プロジェクト内SDKとキャッシュを使う実行補助
docs/                    設計判断・手順・検証記録
TODO.md                  作業順と進捗
```

## 記録

- [フェーズ1の構成案・到達点](docs/phase1-design.md)
- [初期の設計判断](docs/adr/0001-initial-architecture.md)
- [Go・OIDCへの変更](docs/adr/0002-go-and-oidc.md)
- [開発環境とインストール対象](docs/runbooks/development-environment.md)
- [ステップ1の検証結果](docs/verification/01-go-foundation.md)

UIは不要なセカンダリラベルや説明を避け、レイアウトで伝える。
