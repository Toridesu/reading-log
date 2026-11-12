package database

import (
	"context"
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
