-- ログイン途中の確認情報と、画面に表示するユーザー名を追加する。
-- +goose Up
ALTER TABLE users ADD COLUMN display_name text NOT NULL DEFAULT '' CHECK (length(display_name) <= 200);

-- stateは認証の往復、browser_hashは開始したブラウザーとの結び付きを確認する。
-- nonceとPKCE verifierは認証応答の検証・コード交換に使い、5分で失効させる。
CREATE TABLE login_requests (
    state_hash bytea PRIMARY KEY CHECK (octet_length(state_hash) = 32),
    browser_hash bytea NOT NULL CHECK (octet_length(browser_hash) = 32),
    nonce text NOT NULL,
    verifier text NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE INDEX login_requests_expires_idx ON login_requests(expires_at);

-- +goose Down
DROP TABLE login_requests;
ALTER TABLE users DROP COLUMN display_name;
