-- 初回のテーブル定義。Upは適用、Downは取り消し用のSQLを表す。
-- +goose Up
-- users：ログインした人を識別する。認証サービスのURL（issuer）と利用者ID（subject）の組で区別する。
CREATE TABLE users (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    issuer text NOT NULL CHECK (length(issuer) > 0),
    subject text NOT NULL CHECK (length(subject) > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (issuer, subject)
);

-- sessions：ログイン状態とその期限を管理するための保存先。
-- セッションIDそのものではなく、SHA-256のハッシュを保存する。期限の確認はGo側で実装する。
CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    CHECK (expires_at > created_at)
);
CREATE INDEX sessions_user_id_idx ON sessions(user_id);
CREATE INDEX sessions_expires_at_idx ON sessions(expires_at);

-- reading_tasks：書名・読書状況・メモを保存する。user_idが所有者を表す。
-- 他人のタスクを操作させない認可処理は、Go側で別途実装する。
CREATE TABLE reading_tasks (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 300),
    -- unread=未読、reading=読書中、finished=読了。
    status text NOT NULL DEFAULT 'unread' CHECK (status IN ('unread', 'reading', 'finished')),
    notes text NOT NULL DEFAULT '' CHECK (length(notes) <= 10000),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
-- ユーザーごとのタスクを更新日時順に取り出す検索を助ける。
CREATE INDEX reading_tasks_user_updated_idx ON reading_tasks(user_id, updated_at DESC, id DESC);

-- +goose Down
-- テーブル内のデータも消えるため、今回のコマンドではDownを公開していない。
DROP TABLE reading_tasks;
DROP TABLE sessions;
DROP TABLE users;
