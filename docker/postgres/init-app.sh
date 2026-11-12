#!/usr/bin/env bash
# PostgreSQLが空のデータ領域を初期化するときだけ実行する。再起動時には実行しない。
set -euo pipefail

# アプリは管理者ロールを使わず、専用ユーザーで接続する。
psql --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  --set=ON_ERROR_STOP=1 --set=app_password="$APP_DB_PASSWORD" <<'SQL'
CREATE ROLE readinglog LOGIN PASSWORD :'app_password';
GRANT CONNECT ON DATABASE readinglog TO readinglog;
ALTER SCHEMA public OWNER TO readinglog;
-- 自動テストのデータを、アプリで使うreadinglogとは別のDBへ保存する。
CREATE DATABASE readinglog_test OWNER readinglog;
SQL
