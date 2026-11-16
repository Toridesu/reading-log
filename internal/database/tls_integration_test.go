// 専用の使い捨てPostgreSQLでTLSを検証する。scripts/verify-database-tls.pyから実行する。
package database

import (
	"context"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTLSIntegration(t *testing.T) {
	raw := os.Getenv("TLS_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("run scripts/verify-database-tls.py to create a disposable TLS database")
	}
	base, err := url.Parse(raw)
	if err != nil || base.Path != "/readinglog_tls_test" || base.Hostname() != "localhost" {
		t.Fatal("TLS_TEST_DATABASE_URL must point to the disposable local TLS database")
	}
	t.Setenv("DATABASE_REQUIRE_TLS", "true")
	password, _ := base.User.Password()

	t.Run("verified_connection", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		db, err := Open(ctx, raw)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var encrypted bool
		var version string
		if err := db.QueryRowContext(ctx, "SELECT ssl, version FROM pg_stat_ssl WHERE pid=pg_backend_pid()").Scan(&encrypted, &version); err != nil {
			t.Fatal(err)
		}
		if !encrypted || (version != "TLSv1.2" && version != "TLSv1.3") {
			t.Fatal("database did not confirm encrypted transport")
		}
		t.Log("PostgreSQL confirmed", version)
	})

	for _, name := range []string{"wrong_hostname", "untrusted_ca", "missing_ca", "plaintext_fallback", "server_rejects_plaintext"} {
		t.Run(name, func(t *testing.T) {
			u := *base
			query := u.Query()
			switch name {
			case "wrong_hostname":
				u.Host = net.JoinHostPort("127.0.0.1", u.Port())
			case "untrusted_ca":
				query.Set("sslrootcert", os.Getenv("TLS_TEST_UNTRUSTED_CA"))
			case "missing_ca":
				query.Set("sslrootcert", filepath.Join(t.TempDir(), "missing.pem"))
			case "plaintext_fallback":
				query.Set("sslmode", "prefer")
			case "server_rejects_plaintext":
				t.Setenv("DATABASE_REQUIRE_TLS", "false")
				query.Set("sslmode", "disable")
			}
			u.RawQuery = query.Encode()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			db, err := Open(ctx, u.String())
			if db != nil {
				db.Close()
			}
			if err == nil {
				t.Fatal("invalid TLS connection succeeded")
			}
			if password != "" && strings.Contains(err.Error(), password) {
				t.Fatal("password leaked through connection error")
			}
			if name == "server_rejects_plaintext" && !strings.Contains(err.Error(), "database connection failed") {
				t.Fatal("expected rejection during connection, not by client configuration")
			}
		})
	}
}
