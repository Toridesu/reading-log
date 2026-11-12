-- +goose Up
CREATE TABLE users (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    issuer text NOT NULL CHECK (length(issuer) > 0),
    subject text NOT NULL CHECK (length(subject) > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (issuer, subject)
);

-- セッションIDそのものではなく、SHA-256のハッシュを保存する。
CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    CHECK (expires_at > created_at)
);
CREATE INDEX sessions_user_id_idx ON sessions(user_id);
CREATE INDEX sessions_expires_at_idx ON sessions(expires_at);

CREATE TABLE reading_tasks (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 300),
    status text NOT NULL DEFAULT 'unread' CHECK (status IN ('unread', 'reading', 'finished')),
    notes text NOT NULL DEFAULT '' CHECK (length(notes) <= 10000),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX reading_tasks_user_updated_idx ON reading_tasks(user_id, updated_at DESC, id DESC);

-- +goose Down
DROP TABLE reading_tasks;
DROP TABLE sessions;
DROP TABLE users;
