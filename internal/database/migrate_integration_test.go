package database

import (
	"context"
	"crypto/rand"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestMigrationsIntegration(t *testing.T) {
	connectionString := os.Getenv("TEST_DATABASE_URL")
	if connectionString == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	u, err := url.Parse(connectionString)
	if err != nil || u.Path != "/readinglog_test" {
		t.Fatal("TEST_DATABASE_URL must point to readinglog_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := Open(ctx, connectionString)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	// テスト専用DBの一時スキーマだけを使い、アプリのデータに触れない。
	schema := "test_" + strings.ToLower(rand.Text())
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if _, err := admin.ExecContext(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	provider, err := Migrator(db)
	if err != nil {
		t.Fatal(err)
	}
	first, err := provider.Up(ctx)
	if err != nil || len(first) != 1 {
		t.Fatalf("first migration: %v, error: %v", first, err)
	}
	second, err := provider.Up(ctx)
	if err != nil || len(second) != 0 {
		t.Fatalf("second migration: %v, error: %v", second, err)
	}

	var userID int64
	if err := db.QueryRowContext(ctx, "INSERT INTO users (issuer, subject) VALUES ($1, $2) RETURNING id", "https://issuer.example", "user-a").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO users (issuer, subject) VALUES ($1, $2)", "https://other-issuer.example", "user-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO reading_tasks (user_id, title) VALUES ($1, $2)", userID, "テスト用の本"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, now() + interval '1 hour')", make([]byte, 32), userID); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, query, code string
		args              []any
	}{
		{"duplicate identity", "INSERT INTO users (issuer, subject) VALUES ($1, $2)", "23505", []any{"https://issuer.example", "user-a"}},
		{"missing owner", "INSERT INTO reading_tasks (user_id, title) VALUES (-1, 'book')", "23503", nil},
		{"empty title", "INSERT INTO reading_tasks (user_id, title) VALUES ($1, '   ')", "23514", []any{userID}},
		{"invalid status", "INSERT INTO reading_tasks (user_id, title, status) VALUES ($1, 'book', 'unknown')", "23514", []any{userID}},
		{"short session hash", "INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, now() + interval '1 hour')", "23514", []any{[]byte{1}, userID}},
		{"expired session", "INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, now() - interval '1 hour')", "23514", []any{make([]byte, 32), userID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.ExecContext(ctx, tc.query, tc.args...)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tc.code {
				t.Fatalf("expected SQLSTATE %s, got %v", tc.code, err)
			}
		})
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM users WHERE id=$1", userID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"sessions", "reading_tasks"} {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("cascade %s: count=%d, err=%v", table, count, err)
		}
	}
}
