// DBを起動せずに、不正な接続設定の拒否とパスワードをエラーへ出さないことを確認する。
package database

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestOpenRejectsInvalidConfigWithoutLeakingCredentials(t *testing.T) {
	for _, input := range []string{"", "postgres://readinglog:secret-password@localhost/db?connect_timeout=invalid"} {
		db, err := Open(context.Background(), input)
		if db != nil || err == nil {
			t.Fatal("expected configuration error")
		}
		if strings.Contains(err.Error(), "secret-password") {
			t.Fatal("credentials leaked through error")
		}
	}
}

func TestConnectionConfigRequiresVerifiedTLS(t *testing.T) {
	for _, key := range []string{"PGSSLMODE", "PGSSLROOTCERT", "PGSSLCERT", "PGSSLKEY", "PGSERVICE"} {
		t.Setenv(key, "")
	}
	for _, mode := range []string{"", "disable", "allow", "prefer", "require", "verify-ca"} {
		t.Run("reject_"+mode, func(t *testing.T) {
			_, err := connectionConfig("postgres://user:secret-password@db.example/readinglog?sslmode="+mode, true)
			if err == nil {
				t.Fatal("unverified or plaintext connection accepted")
			}
			if strings.Contains(err.Error(), "secret-password") {
				t.Fatal("credentials leaked through config error")
			}
		})
	}
	config, err := connectionConfig("postgres://user:secret-password@db.example/readinglog?sslmode=verify-full", true)
	if err != nil || config.TLSConfig == nil || config.TLSConfig.InsecureSkipVerify || config.TLSConfig.ServerName != "db.example" {
		t.Fatalf("verified TLS configuration: %v", err)
	}
	local, err := connectionConfig("postgres://user:secret-password@localhost/readinglog?sslmode=disable", false)
	if err != nil || local.TLSConfig != nil {
		t.Fatalf("explicit local plaintext exception: %v", err)
	}
	_, err = connectionConfig("postgres://user:secret-password@db.example/readinglog?sslmode=verify-full&sslrootcert=/missing-secret-password.pem", true)
	if err == nil || strings.Contains(err.Error(), "secret-password") {
		t.Fatal("missing CA must be rejected without leaking path or credentials")
	}
}

func TestOpenDefaultsToTLSAndRejectsInvalidSwitch(t *testing.T) {
	// t.Setenvの後でunsetし、終了時の復元はテストの仕組みに任せる。
	t.Setenv("DATABASE_REQUIRE_TLS", "true")
	if err := os.Unsetenv("DATABASE_REQUIRE_TLS"); err != nil {
		t.Fatal(err)
	}
	_, err := Open(context.Background(), "postgres://user:secret-password@127.0.0.1:1/db?sslmode=disable")
	if err == nil || !strings.Contains(err.Error(), "verify-full") {
		t.Fatal("TLS must be required when the switch is absent")
	}
	t.Setenv("DATABASE_REQUIRE_TLS", "invalid-secret-password")
	_, err = Open(context.Background(), "postgres://user:secret-password@localhost/db?sslmode=disable")
	if err == nil || strings.Contains(err.Error(), "secret-password") {
		t.Fatal("invalid TLS switch must fail without echoing its value")
	}
}
