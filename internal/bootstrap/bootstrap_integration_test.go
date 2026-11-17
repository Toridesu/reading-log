// 使い捨てTLS DBとメモリ上のSecretで、初期化・再実行・障害復旧を検証する。
package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"readinglog/internal/database"
)

type memorySecrets struct {
	values               map[string]string
	writes               int
	failWrite, loseReply bool
}

func (s *memorySecrets) Read(_ context.Context, id string) (string, error) {
	value, ok := s.values[id]
	if !ok {
		return "", ErrNoValue
	}
	return value, nil
}
func (s *memorySecrets) Write(_ context.Context, id, value, token string) error {
	if s.failWrite {
		return errors.New("secret-password write failed")
	}
	s.values[id] = value
	s.writes++
	if s.loseReply {
		return errors.New("secret-password response lost")
	}
	return nil
}

func TestBootstrapIntegration(t *testing.T) {
	raw := os.Getenv("BOOTSTRAP_TEST_ADMIN_URL")
	if raw == "" {
		t.Skip("run scripts/verify-database-tls.py --bootstrap")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() != "localhost" || u.Path != "/readinglog" || u.Query().Get("sslmode") != "verify-full" {
		t.Fatal("requires disposable localhost TLS database")
	}
	t.Setenv("DATABASE_REQUIRE_TLS", "true")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	root, err := database.Open(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	adminPassword, err := randomHex()
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"CREATE ROLE readinglog_admin LOGIN CREATEDB CREATEROLE PASSWORD '" + adminPassword + "'",
		"ALTER DATABASE readinglog OWNER TO readinglog_admin",
	} {
		if _, err := root.ExecContext(ctx, query); err != nil {
			t.Fatal("prepare disposable administrator failed")
		}
	}
	c := Config{Host: u.Hostname(), Port: u.Port(), RootCert: u.Query().Get("sslrootcert"), MasterSecretARN: "master", ApplicationSecretARN: "app"}
	master, _ := json.Marshal(map[string]string{"username": "readinglog_admin", "password": adminPassword})
	store := &memorySecrets{values: map[string]string{"master": string(master)}}
	roleExists := func() bool {
		var exists bool
		if err := root.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'readinglog')").Scan(&exists); err != nil {
			t.Fatal(err)
		}
		return exists
	}
	expectFailure := func(fragment string) {
		err := Run(ctx, c, store)
		if err == nil || !strings.Contains(err.Error(), fragment) || strings.Contains(err.Error(), adminPassword) || strings.Contains(err.Error(), "secret-password") {
			t.Fatalf("expected sanitized error containing %s", fragment)
		}
	}
	t.Run("failed_secret_write_leaves_no_role", func(t *testing.T) {
		store.failWrite = true
		expectFailure("save application secret failed")
		if roleExists() || store.writes != 0 {
			t.Fatal("database changed before secret save succeeded")
		}
		store.failWrite = false
	})
	t.Run("lost_write_response_is_recoverable", func(t *testing.T) {
		store.loseReply = true
		expectFailure("save application secret failed")
		if roleExists() || store.writes != 1 || store.values["app"] == "" {
			t.Fatal("unexpected state after lost response")
		}
		store.loseReply = false
	})
	saved := store.values["app"]
	t.Run("database_failure_preserves_secret_and_rolls_back", func(t *testing.T) {
		if _, err := root.ExecContext(ctx, "ALTER ROLE readinglog_admin NOCREATEROLE"); err != nil {
			t.Fatal(err)
		}
		expectFailure("create application role failed")
		if roleExists() || store.values["app"] != saved || store.writes != 1 {
			t.Fatal("retry credentials were changed")
		}
		if _, err := root.ExecContext(ctx, "ALTER ROLE readinglog_admin CREATEROLE"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("permission_failure_rolls_back_created_role", func(t *testing.T) {
		// 空のpublicを一時的に除き、CREATE ROLE後の権限設定を失敗させる。
		if _, err := root.ExecContext(ctx, "DROP SCHEMA public"); err != nil {
			t.Fatal(err)
		}
		expectFailure("configure application permissions failed")
		if roleExists() || store.values["app"] != saved {
			t.Fatal("partially created role was not rolled back")
		}
		if _, err := root.ExecContext(ctx, "CREATE SCHEMA public AUTHORIZATION pg_database_owner"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("initialize_and_repeat", func(t *testing.T) {
		for range 2 {
			if err := Run(ctx, c, store); err != nil {
				t.Fatal(err)
			}
		}
		if store.writes != 1 || store.values["app"] != saved {
			t.Fatal("repeated initialization replaced the secret")
		}
	})
	app, err := database.Open(ctx, saved)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	t.Run("migrate_and_write_as_application", func(t *testing.T) {
		migrator, err := database.Migrator(app)
		if err != nil {
			t.Fatal(err)
		}
		applied, err := migrator.Up(ctx)
		if err != nil || len(applied) != 2 {
			t.Fatal("application migrations failed")
		}
		again, err := migrator.Up(ctx)
		if err != nil || len(again) != 0 {
			t.Fatal("repeat migration failed")
		}
		var id int64
		if err := app.QueryRowContext(ctx, "INSERT INTO users (issuer, subject) VALUES ('https://example.test', 'bootstrap') RETURNING id").Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := app.ExecContext(ctx, "INSERT INTO reading_tasks (user_id, title) VALUES ($1, '初期化検証')", id); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("administration_is_denied", func(t *testing.T) {
		for _, query := range []string{"CREATE ROLE forbidden_role", "CREATE DATABASE forbidden_db", "CREATE SCHEMA forbidden_schema"} {
			_, err := app.ExecContext(ctx, query)
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "42501" {
				t.Fatal("administration was not denied")
			}
		}
	})
	t.Run("wrong_password_is_not_reset", func(t *testing.T) {
		store.values["app"] = c.connectionURL("readinglog", "incorrect-secret-password")
		expectFailure("existing application credentials do not work")
		store.values["app"] = saved
		if err := Run(ctx, c, store); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("missing_secret_with_existing_role_is_rejected", func(t *testing.T) {
		delete(store.values, "app")
		expectFailure("application role exists but its secret is missing")
		store.values["app"] = saved
	})
	t.Run("unexpected_role_privileges_are_rejected", func(t *testing.T) {
		if _, err := root.ExecContext(ctx, "ALTER ROLE readinglog CREATEROLE"); err != nil {
			t.Fatal(err)
		}
		expectFailure("unexpected privileges")
		if _, err := root.ExecContext(ctx, "ALTER ROLE readinglog NOCREATEROLE"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("concurrent_initialization_is_rejected", func(t *testing.T) {
		conn, err := root.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", lockID); err != nil {
			t.Fatal(err)
		}
		expectFailure("already running")
		if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", lockID); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("data_survives_reinitialization", func(t *testing.T) {
		if err := Run(ctx, c, store); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := app.QueryRowContext(ctx, "SELECT count(*) FROM reading_tasks").Scan(&count); err != nil || count != 1 {
			t.Fatal("application data changed")
		}
		if store.writes != 1 {
			t.Fatal("unnecessary secret versions were created")
		}
	})
}
