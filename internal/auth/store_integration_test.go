// 認証のDB操作を専用テストDBで確認する。実際のアプリのデータには触れない。
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"readinglog/internal/database"
)

func TestPostgresAuthentication(t *testing.T) {
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Path != "/readinglog_test" {
		t.Fatal("use readinglog_test only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.Open(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	schema := "auth_" + strings.ToLower(rand.Text())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		c, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if _, err := db.ExecContext(c, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	testDB, err := database.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer testDB.Close()
	migrator, err := database.Migrator(testDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Up(ctx); err != nil {
		t.Fatal(err)
	}
	store := PostgresStore{DB: testDB}
	state, browser := oauth2.GenerateVerifier(), oauth2.GenerateVerifier()
	flow := LoginRequest{Nonce: oauth2.GenerateVerifier(), Verifier: oauth2.GenerateVerifier()}
	if err := store.StartLogin(ctx, state, browser, flow); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeLogin(ctx, state, "wrong"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("wrong browser accepted")
	}
	got, err := store.ConsumeLogin(ctx, state, browser)
	if err != nil || got != flow {
		t.Fatal("flow not consumed")
	}
	if _, err := store.ConsumeLogin(ctx, state, browser); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("flow replay accepted")
	}
	if err := store.StartLogin(ctx, state, browser, flow); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.ExecContext(ctx, "UPDATE login_requests SET expires_at=now()-interval '1 minute'"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeLogin(ctx, state, browser); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("expired flow accepted")
	}
	a, b, rotated := oauth2.GenerateVerifier(), oauth2.GenerateVerifier(), oauth2.GenerateVerifier()
	identity := Identity{"https://issuer.example", "same-subject", "alice"}
	if err := store.CreateSession(ctx, identity, a, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(ctx, Identity{"https://other.example", "same-subject", "bob"}, b, ""); err != nil {
		t.Fatal(err)
	}
	sa, err := store.FindSession(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := store.FindSession(ctx, b)
	if err != nil || sa.UserID == sb.UserID {
		t.Fatal("issuer identities mixed")
	}
	var saved []byte
	if err := testDB.QueryRowContext(ctx, "SELECT token_hash FROM sessions WHERE user_id=$1", sa.UserID).Scan(&saved); err != nil || string(saved) != string(hash(a)) {
		t.Fatal("session must be hashed")
	}
	if err := store.CreateSession(ctx, identity, rotated, a); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindSession(ctx, a); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("old session survived login rotation")
	}
	sr, err := store.FindSession(ctx, rotated)
	if err != nil || sr.UserID != sa.UserID {
		t.Fatal("user mapping changed")
	}
	if err := store.DeleteSession(ctx, rotated); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindSession(ctx, rotated); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("deleted session accepted")
	}
	if _, err := testDB.ExecContext(ctx, "UPDATE sessions SET created_at=now()-interval '2 hours',expires_at=now()-interval '1 hour'"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindSession(ctx, b); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("expired session accepted")
	}
}
