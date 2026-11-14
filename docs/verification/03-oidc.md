# ステップ3：ローカルOIDC認証

実施日：2026-11-14（日本時間）。Go 1.27.4、PostgreSQL 18.6、Keycloak 26.8.2、Playwright 1.65.2 / Chromium Headless Shell 159.0.8154.6をWSLで使用した。

## 結果

| 確認 | 実測結果 |
| --- | --- |
| Keycloak初回起動 | reading-log realmとconfidential client、alice/bobを読み込み、healthyになった |
| マイグレーション | 既存のステップ2環境へ00002を1件適用。実DBテストでは空のスキーマに2件適用し、再適用は0件 |
| Goテスト・静的検証・ビルド | `scripts/dev.sh test ./...`、`scripts/go.sh vet ./...`、`build -o bin/readinglog ./cmd/web` 成功 |
| 認証開始 | state、nonce、PKCE S256、固定redirect URI、prompt=loginを送信。開始用CookieはHttpOnly / SameSite=Lax / 5分 |
| コード交換 | テスト用IdPでclient認証・redirect URI・正しいPKCE verifierの送信を確認。実KeycloakのPKCE必須クライアントでも認証成功 |
| 拒否パターン | state改ざん、Cookie欠落・別ブラウザー、5分を過ぎたフロー、認証応答の再送、コード交換失敗、IDトークン欠落を拒否 |
| IDトークン検証 | 不正署名・issuer・audience・期限・nonce・subject・発行時刻・azpを拒否。拒否時はセッションを作らない |
| DBの認証操作 | state＋ブラウザーを照合し一度だけ取得。同じsubjectでもissuerが違えば別ユーザー。セッションはハッシュで保存 |
| セッション失効 | 再ログイン時の旧セッション削除、期限切れ、ログアウトでの削除を実DBで確認 |
| Cookie | ローカルはHttpOnly / SameSite=Lax、固定8時間。HTTPS設定ではSecure / __Host-接頭辞 / Domainなしを単体テストで確認 |
| ログアウトのCSRF | 異なるOrigin、Origin欠落、CSRF欠落・改ざんを拒否。GETでのログアウトは405 |
| ログ・障害 | 認証コード等をログ・エラー本文へ出さない。DB失敗はアクセスを許可せず、認証処理の負荷上限を超えると429 |
| HTTPでの一連の検証 | `python3 scripts/verify-login.py` でalice/bobのログイン・再送拒否・CSRF拒否・ログアウト・旧Cookie拒否が成功 |
| 実ブラウザー | Chromiumでalice/bobのログイン・ログアウト、未認証アクセスと旧Cookieの拒否を確認 |
| 画面 | 幅1200pxと390pxで確認。ユーザー名とログアウトを表示し、横方向へのはみ出しなし。表示名はHTMLエスケープ |

## ブラウザーで見つかった問題と修正

最初のHTTP検証ではKeycloakがlocalhostに発行するSecure Cookieをurllibが送らず、cookie_not_foundになった。検証スクリプトだけをブラウザーと同じlocalhost例外へ合わせ、Keycloakの設定は変更していない。Chromiumでも成功を確認した。

全ページにReferrer-Policy: no-referrerを付けると、ChromiumのフォームPOSTでOriginがnullになり、正常なログアウトが拒否された。通常ページはsame-origin、認証関連の応答はno-referrerにして、同一Origin＋CSRFの検証を維持した。修正後はHTTP・ブラウザー両方の検証が成功した。

検証用Linux環境に日本語フォントがなかったため、プロジェクト内だけにフォントを用意し、日本語表示を確認した。

## 画面

[ログイン前](images/03-home.png)・[Keycloak](images/03-keycloak.png)・[ログイン後](images/03-books.png)・[モバイル幅](images/03-mobile.png)。いずれもダミーユーザーを使用し、パスワードやトークンを画像へ含めていない。

## 検証範囲

- race検査はこのWSLにCコンパイラがなく、CGO無効のため未実施。通常のGoテスト、実DBテスト、vet、ビルド、HTTPと実ブラウザーの検証は成功した。
- HTTPS時のCookie属性は単体テストで確認。ローカルの通信自体はHTTPで、AWS上のTLS・Cognitoは未検証。
- ログアウトはアプリ内のセッションのみ。IdP側のSSO失効、全端末ログアウト、MFA、鍵ローテーションは未実装・未検証。
- 読書タスクのCRUDと所有者による認可は次のステップ。今回の認証だけではタスク単位のアクセス制御は完成していない。
- 検証の最後にalice/bobのアプリセッションを削除した。ユーザーの対応付けは残している。アプリは検証後に停止し、PostgreSQLとKeycloakは起動状態で残した。

[操作手順](../runbooks/local-auth.md)・[設計判断と通信図](../adr/0003-local-auth-and-sessions.md)
