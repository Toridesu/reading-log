# RDS接続用の公開CA

取得日：2026-11-16（日本時間）

`ap-northeast-1-bundle.pem` はAWS公式の東京リージョン用RDSルートCAバンドル。公開証明書であり、秘密鍵やDBパスワードは含まない。

- [公式のCA一覧と配布案内](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/UsingWithRDS.SSL.html)
- [取得元のPEM](https://truststore.pki.rds.amazonaws.com/ap-northeast-1/ap-northeast-1-bundle.pem)

HTTPSで取得後、3つのルートCAのCA属性・有効期間・自己署名をOpenSSLで確認した。保存した内容のSHA-256は `SHA256SUMS` に固定し、Dockerビルド時にも照合する。AWSが別途公表したチェックサムとの照合ではない。

| 東京ルートCA | NotAfter（UTC） |
| --- | --- |
| RSA2048 G1 | 2061-05-25 22:54:58 |
| RSA4096 G1 | 2121-05-25 22:59:10 |
| ECC384 G1 | 2121-05-25 23:03:16 |

Web・migrateイメージの `/app/certs/ap-northeast-1-bundle.pem` にコピーする。アプリはDB接続URLの `sslrootcert` から参照し、OIDC用の一般的なCAストアへ混ぜない。

更新時は公式配布とRDS側で使うCAを確認し、取得した内容・ルートCAの有効性・差分を検査してからPEMとSHA256SUMSを一緒に更新する。更新後にGoテスト、使い捨てDBのTLS検証、イメージビルドを行う。サーバー証明書の更新と、信頼するルートCAの更新は区別する。
