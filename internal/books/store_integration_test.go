// 実DBで2ユーザーのCRUDと所有者の境界を検証する。専用DBの一時スキーマだけを使う。
package books

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"readinglog/internal/database"
)

func TestPostgresBookOwnership(t *testing.T) {
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
	schema := "books_" + strings.ToLower(rand.Text())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if _, err := db.ExecContext(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
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
	var alice, bob int64
	for name, id := range map[string]*int64{"alice": &alice, "bob": &bob} {
		if err := testDB.QueryRowContext(ctx, "INSERT INTO users (issuer,subject) VALUES ('https://test.example',$1) RETURNING id", name).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	store := PostgresStore{DB: testDB}
	input := Input{Title: "Goの本 ' ; DROP TABLE reading_tasks; --", Status: "unread", Notes: "自分のメモ\n<script>window.attack=true</script>"}
	id, err := store.Create(ctx, alice, input)
	if err != nil {
		t.Fatal(err)
	}
	original, err := store.Get(ctx, alice, id)
	if err != nil || original.Title != input.Title || original.Notes != input.Notes {
		t.Fatal("create/read mismatch")
	}
	if _, err := store.Get(ctx, bob, id); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign read accepted")
	}
	if err := store.Update(ctx, bob, id, Input{"改ざん", "finished", "改ざん"}); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign update accepted")
	}
	if err := store.Delete(ctx, bob, id); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign delete accepted")
	}
	after, err := store.Get(ctx, alice, id)
	if err != nil || after != original {
		t.Fatal("foreign access changed data")
	}
	rows, more, err := store.List(ctx, bob, 1)
	if err != nil || len(rows) != 0 || more {
		t.Fatal("foreign book in list")
	}
	if err := store.Update(ctx, alice, id, Input{"改訂版Goの本", "finished", "読み終えた"}); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Get(ctx, alice, id)
	if err != nil || updated.Title != "改訂版Goの本" || updated.Status != "finished" || updated.Notes != "読み終えた" || !updated.UpdatedAt.After(original.UpdatedAt) {
		t.Fatal("update mismatch")
	}
	var owner int64
	if err := testDB.QueryRowContext(ctx, "SELECT user_id FROM reading_tasks WHERE id=$1", id).Scan(&owner); err != nil || owner != alice {
		t.Fatal("owner changed")
	}
	if err := store.Delete(ctx, alice, id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, alice, id); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted book still readable")
	}
	if err := store.Delete(ctx, alice, id); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing book deletion succeeded")
	}
	// 21件あっても1ページで読み込むのは20件。ページをまたいでも所有者の条件は変わらない。
	ids := map[int64]bool{}
	for i := 0; i < PageSize+1; i++ {
		id, err := store.Create(ctx, bob, Input{fmt.Sprintf("本%d", i), "unread", "大きなメモ"})
		if err != nil {
			t.Fatal(err)
		}
		ids[id] = true
	}
	first, more, err := store.List(ctx, bob, 1)
	if err != nil || len(first) != PageSize || !more {
		t.Fatal("first page incorrect")
	}
	second, more, err := store.List(ctx, bob, 2)
	if err != nil || len(second) != 1 || more {
		t.Fatal("second page incorrect")
	}
	for _, b := range append(first, second...) {
		if !ids[b.ID] || b.Notes != "" {
			t.Fatal("pagination duplicated a book or loaded notes")
		}
		delete(ids, b.ID)
	}
	if len(ids) != 0 {
		t.Fatal("pagination omitted a book")
	}
	rows, _, err = store.List(ctx, alice, 1)
	if err != nil || len(rows) != 0 {
		t.Fatal("other user's books leaked")
	}
}
