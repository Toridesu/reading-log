# ADR-0002：GoとOIDCの採用

日付：2026-11-11（日本時間）

状態：採用する設計方針。Goは利用者指定、認証方式はクラウドセキュリティの学習目的に合わせて選定。Goアプリの骨組みは実装・確認済み。OIDCの実装・検証はこれから行う。

## 背景

利用者からGoで実装する指定と、実務の力になる認証方式を選ぶ依頼があった。AWSリソースを当面作成しない制約は維持する。

## 決定

- アプリはGoの `net/http` と `html/template`、DBはPostgreSQL。DBアクセスにはpgxを使用する。
- 認証方式はOpenID Connect（OIDC）のAuthorization Code Flow + PKCE S256。
- Goサーバーをconfidential clientとし、クライアントシークレットはサーバー側だけに置く。PKCEも併用する。
- ローカルのIdPはDocker Compose上のKeycloak。AWS展開時はCognito User Poolのmanaged loginを使用する。Cognito Identity Poolは初期版では使用しない。
- Go側のOIDC・OAuth2処理には既存ライブラリを使う。パスワード保存や署名アルゴリズムを独自実装しない。
- アプリは検証済みの `iss` と `sub` を内部ユーザーIDへ対応づけ、DB上の所有者による認可を実装する。
- ブラウザーはアプリのセッションCookieを使う。セッションをDB側で失効できるようにし、ID/アクセストークンをブラウザーの永続ストレージへ置かない。

## 学ぶことと検証

| 学習対象 | 完了を判断する検証 |
| --- | --- |
| OAuth2とOIDCの役割 | 認証コード、IDトークン、アクセストークン、アプリのセッションの用途を区別して説明できる |
| 認証フロー | redirect URIを固定し、state・nonce・PKCEが欠ける/一致しない処理を拒否する |
| トークン検証 | 署名・issuer・audience・期限が不正なIDトークンを拒否する。文字列をdecodeしただけで信頼しない |
| セッション管理 | ログインでセッションを更新し、期限切れ・ログアウト後のセッションを拒否する。IdP側のログアウトとの差を確認する |
| 認可 | 他人のタスクの参照・更新・削除、所有者IDの改ざんを拒否し、データが変わらないことを確認する |
| 調査 | request_idからログイン失敗・認可拒否を追える。コード・トークン・Cookieはログへ出さない |

最初は認証フローとセッションを完成させ、次にCRUDと所有者確認を追加する。MFA・鍵更新・セッション失効の運用は最小版完成後の拡張とする。

## 代替案と採用理由

| 案 | 利点 | 今回の判断 |
| --- | --- | --- |
| 自前のパスワード認証 | ハッシュ、ロックアウト、復旧の仕組みを深く学べる | 個人アプリに認証基盤運用まで持たせる。今回は標準プロトコルによる境界の設計・検証を優先 |
| Cognitoへ直接接続して開発 | AWSの認証機能をそのまま確認できる | 現時点でAWSリソースを作成しない条件と合わない |
| ローカルKeycloak → AWS Cognito | AWSを作らず実際のOIDCフローを試せる。IdPへの依存箇所を学べる | 採用。2種類の設定と差分検証が必要になる |
| ALBの認証機能に委譲 | アプリの認証連携コードを減らせる | 今回はアプリでコード交換・トークン検証・セッションを学ぶ。比較演習としては有用 |

KeycloakはCognitoのエミュレーターではない。ユーザー、issuer、トークンの属性、ログアウト等の差はAWS展開時に検証する。プロバイダー間のユーザー対応をメールアドレスだけで自動連携しない。

## ネットワークへの影響

CognitoのOAuthドメインを使用するコード交換はPrivateLinkを経由できない。以前の「外向き通信はVPCエンドポイントだけ」という案を変更し、初期AWS構成はNAT Gateway + S3 Gateway Endpointとする。アプリとRDSはプライベートのままにする。

初期はNAT Gatewayを1AZに置く。単一AZへの依存とAZをまたぐ通信を記録し、可用性を拡張するときに経路を見直す。NATはドメイン単位の送信先制御を行う仕組みではない。ECR・Logs・SecretsへのInterface Endpointは後から追加比較する。

## 公式資料

- [Go HTTP](https://pkg.go.dev/net/http)
- [Go OAuth2 / PKCE](https://pkg.go.dev/golang.org/x/oauth2)
- [go-oidc](https://github.com/coreos/go-oidc)
- [CognitoのPKCE](https://docs.aws.amazon.com/cognito/latest/developerguide/using-pkce-in-authorization-code.html)
- [CognitoのPrivateLink対応範囲](https://docs.aws.amazon.com/cognito/latest/developerguide/vpc-interface-endpoints.html)
- [Keycloakのコンテナ起動](https://www.keycloak.org/server/containers)

参照日：2026-11-11。
