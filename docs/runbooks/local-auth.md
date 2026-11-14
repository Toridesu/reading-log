# ローカルのログイン・ログアウト

## 起動

Docker Desktopを起動して、プロジェクトディレクトリで実行する。設定が既にある場合もsetup-local.shを実行すれば、既存の値を保持して不足分だけ追加する。

```bash
bash scripts/setup-local.sh
docker compose up -d --wait db keycloak
bash scripts/dev.sh run -buildvcs=false ./cmd/migrate up
bash scripts/dev.sh run -buildvcs=false ./cmd/web
```

[アプリ](http://localhost:8080)からログインする。ユーザー名は `alice` または `bob`、パスワードは手元の `.env` の `LOCAL_USER_PASSWORD`。同じパスワードを使うローカル検証専用の2ユーザーで、実運用には引き継がない。

[Keycloak管理画面](http://localhost:8081/admin/)の管理者は `admin`、パスワードは `.env` の `KEYCLOAK_ADMIN_PASSWORD`。realmの切り替え先は `reading-log`。Usersに2ユーザー、Clientsに同名のアプリ用クライアントがある。

ブラウザーは必ず `localhost` で開く。GoもKeycloakも同じissuer `http://localhost:8081/realms/reading-log` を参照する。GoはWSL上で直接実行し、KeycloakはComposeからlocalhostへ公開する構成。アプリをコンテナ化するときは、この通信経路を再設計する。

## どのファイルを見ればよいか

| ファイル | 役割 |
| --- | --- |
| `docker/keycloak/reading-log-realm.json` | realm（認証設定のまとまり）、クライアント、検証ユーザーの初期定義。秘密値は環境変数から読む |
| `internal/auth/config.go` | 接続先とクライアント情報。外部ホストのHTTPや不正なURLを拒否 |
| `internal/auth/service.go` | ログイン開始・コールバック・Cookie・ログアウト |
| `internal/auth/store.go` | 認証途中の情報、ユーザー、セッションのDB操作 |
| `migrations/00002_login.sql` | ログイン途中の情報と表示名の保存先 |
| `internal/web/handler.go` | 公開ページとログイン必須ページの振り分け |

## 動作確認

1. 未ログインで `/books` を開くとトップへ戻る。
2. `ログイン` を押し、aliceで認証するとページ右上にaliceとログアウトボタンが出る。
3. ログアウト後に `/books` を直接開いてもトップへ戻る。
4. bobでも同じ流れを確認する。

ログアウトはアプリのセッションを失効させる。Keycloak側のSSOセッションは残るが、次のログインでパスワード入力を要求する。ログイン後は[読書タスクの作成・一覧・編集・削除](reading-tasks.md)を使える。

```bash
# Go単体テスト。テスト用IdPで署名付きトークンを使う。実Keycloakは不要
bash scripts/go.sh test ./...
bash scripts/go.sh vet ./...

# DB起動後：実DBのセッション・期限・失効・一度だけの取り出しも検証
bash scripts/dev.sh test ./...

# アプリ・DB・Keycloak起動後：実ログインフォームを2ユーザーで通す
python3 scripts/verify-login.py
```

HTTP検証はPython標準ライブラリだけで動く。KeycloakのSecure Cookieについて、localhostの例外を持つブラウザーの挙動に合わせている。実ブラウザー用の `scripts/verify-browser.py` もあり、PlaywrightとChromiumを用意した環境で実行できる。画像はGit管理対象外の `.cache/verification/` へ保存する。

## ログ・設定の確認

```bash
docker compose ps
docker compose logs --tail 50 keycloak
docker compose logs --tail 50 db
bash scripts/dev.sh run -buildvcs=false ./cmd/migrate status
```

アプリのJSONログは起動したターミナルに出る。`login_succeeded`、`logout_succeeded`、`auth_rejected` を、同じ `request_id` の `http_request` と照合する。失敗理由は `reason` に固定のコードで出し、code・state・nonce・Cookie・Token・パスワードは記録しない。

| 状況・reason | 確認するもの |
| --- | --- |
| 起動時OIDC discovery failed | Keycloakのhealthy状態、OIDC_ISSUER、localhost:8081への接続 |
| invalid_host | ブラウザーのURLとAPP_BASE_URL。localhostと127.0.0.1の混在 |
| Keycloakでredirect URIエラー | Clients → reading-logのValid redirect URIsとAPP_BASE_URL/auth/callback |
| missing_login_cookie / invalid_or_expired_login | 同じブラウザーから開始したか、5分経過・再送・別タブでのログイン開始がないか |
| code_exchange_failed | クライアントシークレットの一致、Keycloak稼働、認証コードの再利用がないか |
| invalid_id_token / invalid_identity_claims | issuer、client ID、サーバー時刻、nonce、署名鍵 |
| invalid_origin / invalid_csrf | 正規ページからPOSTしたか、古いページや別サイトからの送信でないか |
| session_store_failed / session_unavailable | DB状態とマイグレーション。DB障害時に未認証として通過させない |
| rate_limited | 短時間の認証リクエスト数。1秒待って再実行 |

セッションの有効期限は8時間。`/readyz` はDB接続の確認で、Keycloakの稼働確認は含まない。

## 設定変更・停止・削除

ポートを変更するときは `APP_ADDR`、`APP_BASE_URL`、`KEYCLOAK_PORT`、`OIDC_ISSUER` とKeycloakのredirect URIを揃える。クライアントIDを変える場合はrealm定義・既存クライアントとGoの設定の両方を変更する。

Keycloakの `--import-realm` は既存realmを上書きしない。`.env` やrealm JSONを編集して再起動しても、既存クライアントの秘密値・ユーザーのパスワードは更新されない。管理画面で対象設定を変更するか、検証データを捨ててよい場合だけKeycloakのボリュームを作り直す。

通常の停止は `docker compose stop`。アプリはCtrl+C。コンテナとネットワークだけを削除する場合は `docker compose down`。どちらもボリュームのデータは残る。

**Keycloakの検証データだけを捨てて初期化する場合：**

```bash
docker compose rm -sf keycloak
docker volume rm reading-log_keycloak_data
docker compose up -d --wait keycloak
```

Keycloakを初期化すると利用者のsubjectや署名鍵が変わるため、アプリDBの古いユーザーと自動で結び付けない。アプリセッションの失効や検証用データの整理も別途行う。

`docker compose down -v` はPostgreSQLとKeycloakの両方のデータを消す。全体を最初からやり直してよい場合だけ使う。AWSリソースの作成はこの手順に含まない。
