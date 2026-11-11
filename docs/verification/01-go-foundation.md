# ステップ1：Goアプリの骨組み

実施日：2026-11-11（日本時間）
環境：WSL2 / Linux amd64 / Go 1.27.4

| 確認 | 結果 |
| --- | --- |
| Go SDK | 公式配布物をSHA256照合後、プロジェクト内 `.tools/go` に配置 |
| `bash scripts/go.sh fmt ./...` | 整形済み |
| `bash scripts/go.sh test ./...` | 成功。画面・health・CSS・404・405、HTTP応答とログの対応、認証情報をログへ出さないことを確認 |
| `bash scripts/go.sh vet ./...` | 成功 |
| `bash scripts/go.sh build -buildvcs=false -o bin/readinglog ./cmd/web` | 成功 |
| `GET /` | 実サーバーで200、HTMLに「読書ノート」を確認 |
| `GET /healthz` | 実サーバーで200、`{"status":"ok"}` を確認 |
| `GET /static/app.css` | 実サーバーで200、CSSを確認 |
| request_id | レスポンスヘッダーとJSONログで同じIDを確認 |
| Ctrl+C | `server_stopping` → `server_stopped`、終了コード0 |

トップ画面はアプリの骨組みであり、読書データや操作ボタンはまだない。ブラウザータブを開く操作は要求済みだが、画面の目視・スクリーンショットによる確認はこの記録に含めない。

この実行環境では通常のサンドボックスからソケットを開けなかったため、許可された実行で127.0.0.1に限定して起動・HTTP確認を行った。確認後はサーバーを停止した。

Gitメタデータを取得できず通常のビルドが失敗したため、`-buildvcs=false` を指定した。アプリのビルド自体は成功している。

DB・認証・CRUD・Dockerイメージ・AWSの動作はまだ検証していない。次はTODOのステップ2へ進む。
