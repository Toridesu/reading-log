// HTTP入力を改ざんしても、認証済みの所有者とCSRF確認を通った操作だけがDBへ届くことを確かめる。
package web

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"

	"readinglog/internal/auth"
	"readinglog/internal/books"
)

type storedBook struct {
	owner int64
	book  books.Book
}
type memoryBooks struct {
	records map[int64]storedBook
	next    int64
	writes  int
	fail    bool
	owners  []int64
}

func newBookStore() *memoryBooks { return &memoryBooks{records: map[int64]storedBook{}, next: 1} }
func (s *memoryBooks) List(_ context.Context, userID int64, page int) ([]books.Book, bool, error) {
	s.owners = append(s.owners, userID)
	if s.fail {
		return nil, false, errors.New("private-database-error")
	}
	rows := []books.Book{}
	for _, r := range s.records {
		if r.owner == userID {
			rows = append(rows, r.book)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID > rows[j].ID })
	offset := (page - 1) * books.PageSize
	if offset >= len(rows) {
		return []books.Book{}, false, nil
	}
	rows = rows[offset:]
	more := len(rows) > books.PageSize
	if more {
		rows = rows[:books.PageSize]
	}
	return rows, more, nil
}
func (s *memoryBooks) Get(_ context.Context, userID, id int64) (books.Book, error) {
	s.owners = append(s.owners, userID)
	if s.fail {
		return books.Book{}, errors.New("private-database-error")
	}
	r, ok := s.records[id]
	if !ok || r.owner != userID {
		return books.Book{}, books.ErrNotFound
	}
	return r.book, nil
}
func (s *memoryBooks) Create(_ context.Context, userID int64, in books.Input) (int64, error) {
	s.owners = append(s.owners, userID)
	if s.fail {
		return 0, errors.New("private-database-error")
	}
	id := s.next
	s.next++
	s.records[id] = storedBook{userID, books.Book{ID: id, Title: in.Title, Status: in.Status, Notes: in.Notes}}
	s.writes++
	return id, nil
}
func (s *memoryBooks) Update(ctx context.Context, userID, id int64, in books.Input) error {
	b, err := s.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	b.Title, b.Status, b.Notes = in.Title, in.Status, in.Notes
	s.records[id] = storedBook{userID, b}
	s.writes++
	return nil
}
func (s *memoryBooks) Delete(ctx context.Context, userID, id int64) error {
	if _, err := s.Get(ctx, userID, id); err != nil {
		return err
	}
	delete(s.records, id)
	s.writes++
	return nil
}

type bookAuth struct {
	guestAuth
	id  int64
	err error
}

func (a bookAuth) Session(r *http.Request) (auth.Session, error) {
	if a.err != nil {
		return auth.Session{}, a.err
	}
	return auth.Session{UserID: a.id, Name: "reader", CSRFToken: "csrf-" + strconv.FormatInt(a.id, 10)}, nil
}
func bookApp(t *testing.T, store *memoryBooks, login Authentication) (http.Handler, *bytes.Buffer) {
	t.Helper()
	logs := &bytes.Buffer{}
	h, err := NewHandler(slog.New(slog.NewJSONHandler(logs, nil)), func(context.Context) error { return nil }, login, store)
	if err != nil {
		t.Fatal(err)
	}
	return h, logs
}
func requestBook(h http.Handler, method, path string, form url.Values) *httptest.ResponseRecorder {
	body := ""
	if form != nil {
		body = form.Encode()
	}
	r := httptest.NewRequest(method, "http://example.com"+path, strings.NewReader(body))
	if method == "POST" {
		r.Header.Set("Origin", "http://example.com")
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func bookForm(user int64) url.Values {
	return url.Values{"csrf_token": {"csrf-" + strconv.FormatInt(user, 10)}, "title": {"私の本"}, "status": {"unread"}, "notes": {"私のメモ"}}
}

func TestBookCRUD(t *testing.T) {
	store := newBookStore()
	h, logs := bookApp(t, store, bookAuth{id: 1})
	if w := requestBook(h, "GET", "/books/new", nil); w.Code != 200 {
		t.Fatal("new form failed")
	}
	form := bookForm(1)
	form.Set("title", `<script>alert(1)</script>`)
	form.Set("notes", `</textarea><script>attack()</script>`)
	created := requestBook(h, "POST", "/books", form)
	if created.Code != 303 || created.Header().Get("Location") != "/books/1" {
		t.Fatal("create failed")
	}
	for _, path := range []string{"/books", "/books/1", "/books/1/edit"} {
		w := requestBook(h, "GET", path, nil)
		if w.Code != 200 || strings.Contains(w.Body.String(), "<script>") || !strings.Contains(w.Body.String(), "&lt;script&gt;") {
			t.Fatalf("unsafe rendering: %s", path)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("private page cached")
		}
	}
	form.Set("title", "改訂版")
	form.Set("status", "finished")
	form.Set("notes", "更新したメモ")
	if w := requestBook(h, "POST", "/books/1", form); w.Code != 303 || store.records[1].book.Status != "finished" {
		t.Fatal("update failed")
	}
	writes := store.writes
	if w := requestBook(h, "GET", "/books/1/delete", nil); w.Code != 200 || store.writes != writes {
		t.Fatal("GET performed deletion")
	}
	if w := requestBook(h, "POST", "/books/1/delete", url.Values{"csrf_token": {"csrf-1"}}); w.Code != 303 || len(store.records) != 0 {
		t.Fatal("delete failed")
	}
	for _, owner := range store.owners {
		if owner != 1 {
			t.Fatal("wrong owner passed to store")
		}
	}
	for _, secret := range []string{"csrf-1", "私のメモ", "更新したメモ", "alert(1)"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("private content logged")
		}
	}
	for _, event := range []string{"book_created", "book_updated", "book_deleted"} {
		if !strings.Contains(logs.String(), event) {
			t.Fatal("missing operation log")
		}
	}
}

func TestForeignBooksAreNotDisclosedOrChanged(t *testing.T) {
	store := newBookStore()
	_, _ = store.Create(context.Background(), 1, books.Input{Title: "alice-private-title", Status: "reading", Notes: "alice-private-notes"})
	original := store.records[1]
	h, _ := bookApp(t, store, bookAuth{id: 2})
	if w := requestBook(h, "GET", "/books", nil); w.Code != 200 || strings.Contains(w.Body.String(), "alice-private") {
		t.Fatal("foreign book listed")
	}
	missing := requestBook(h, "GET", "/books/999", nil).Body.String()
	for _, path := range []string{"/books/1", "/books/1/edit", "/books/1/delete"} {
		w := requestBook(h, "GET", path, nil)
		if w.Code != 404 || w.Body.String() != missing {
			t.Fatalf("foreign data disclosed: %s", path)
		}
	}
	for _, tc := range []struct {
		path string
		form url.Values
	}{{"/books/1", bookForm(2)}, {"/books/1/delete", url.Values{"csrf_token": {"csrf-2"}}}} {
		w := requestBook(h, "POST", tc.path, tc.form)
		if w.Code != 404 || store.records[1] != original {
			t.Fatal("foreign book modified")
		}
	}
}

func TestBookMutationProtection(t *testing.T) {
	for _, route := range []string{"/books", "/books/1", "/books/1/delete"} {
		for _, kind := range []string{"no csrf", "wrong csrf", "other user's csrf", "no origin", "cross origin", "owner field", "query owner", "duplicate field", "oversized", "wrong content type"} {
			t.Run(route+"/"+kind, func(t *testing.T) {
				store := newBookStore()
				_, _ = store.Create(context.Background(), 1, books.Input{Title: "original", Status: "unread", Notes: "original-note"})
				before := store.records[1]
				writes := store.writes
				h, _ := bookApp(t, store, bookAuth{id: 1})
				form := bookForm(1)
				if strings.HasSuffix(route, "/delete") {
					form = url.Values{"csrf_token": {"csrf-1"}}
				}
				path, origin, contentType, want := route, "http://example.com", "application/x-www-form-urlencoded", 403
				switch kind {
				case "no csrf":
					form.Del("csrf_token")
				case "wrong csrf":
					form.Set("csrf_token", "wrong")
				case "other user's csrf":
					form.Set("csrf_token", "csrf-2")
				case "no origin":
					origin = ""
				case "cross origin":
					origin = "https://evil.example"
				case "owner field":
					form.Set("user_id", "2")
					want = 400
				case "query owner":
					path += "?user_id=2"
					want = 400
				case "duplicate field":
					form.Add("csrf_token", "csrf-1")
				case "oversized":
					form.Set("notes", strings.Repeat("x", 128*1024))
					want = 413
				case "wrong content type":
					contentType = "application/json"
					want = 415
				}
				r := httptest.NewRequest("POST", "http://example.com"+path, strings.NewReader(form.Encode()))
				r.Header.Set("Origin", origin)
				r.Header.Set("Content-Type", contentType)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != want || store.writes != writes || store.records[1] != before {
					t.Fatalf("protection failed: status=%d want=%d", w.Code, want)
				}
			})
		}
	}
}

func TestBookValidation(t *testing.T) {
	for _, route := range []string{"/books", "/books/1"} {
		store := newBookStore()
		_, _ = store.Create(context.Background(), 1, books.Input{Title: "original", Status: "unread", Notes: "old"})
		before := store.records[1]
		h, _ := bookApp(t, store, bookAuth{id: 1})
		form := bookForm(1)
		form.Set("title", " \u3000 ")
		form.Set("status", "unknown")
		form.Set("notes", strings.Repeat("本", 10001))
		w := requestBook(h, "POST", route, form)
		if w.Code != 422 || !strings.Contains(w.Body.String(), "1〜300文字") || !strings.Contains(w.Body.String(), "10,000文字") || store.records[1] != before || len(store.records) != 1 {
			t.Fatal("invalid values saved or missing errors")
		}
	}
	store := newBookStore()
	h, _ := bookApp(t, store, bookAuth{id: 1})
	form := bookForm(1)
	form.Set("title", strings.Repeat("📖", 300))
	form.Set("notes", strings.Repeat("📖", 10000))
	if w := requestBook(h, "POST", "/books", form); w.Code != 303 {
		t.Fatalf("valid multibyte limits rejected: %d", w.Code)
	}
	form = bookForm(1)
	form.Add("title", "another")
	if w := requestBook(h, "POST", "/books", form); w.Code != 400 {
		t.Fatal("duplicate title accepted")
	}
}

func TestBookAuthenticationAndFailures(t *testing.T) {
	routes := []struct{ method, path string }{{"GET", "/books"}, {"GET", "/books/new"}, {"GET", "/books/1"}, {"GET", "/books/1/edit"}, {"GET", "/books/1/delete"}, {"POST", "/books"}, {"POST", "/books/1"}, {"POST", "/books/1/delete"}}
	for _, tc := range routes {
		store := newBookStore()
		h, _ := bookApp(t, store, guestAuth{})
		w := requestBook(h, tc.method, tc.path, bookForm(1))
		if w.Code != 303 || len(store.owners) != 0 {
			t.Fatal("guest reached storage")
		}
		h, logs := bookApp(t, store, bookAuth{id: 1, err: errors.New("private-session-db-error")})
		w = requestBook(h, tc.method, tc.path, bookForm(1))
		if w.Code != 503 || len(store.owners) != 0 || strings.Contains(w.Body.String()+logs.String(), "private-session-db-error") {
			t.Fatal("session error leaked or failed open")
		}
	}
	store := newBookStore()
	store.fail = true
	h, logs := bookApp(t, store, bookAuth{id: 1})
	w := requestBook(h, "GET", "/books", nil)
	if w.Code != 503 || strings.Contains(w.Body.String()+logs.String(), "private-database-error") {
		t.Fatal("DB error exposed")
	}
	store.fail = false
	for _, path := range []string{"/books/nope", "/books/-1", "/books/0", "/books/99999999999999999999999"} {
		if w := requestBook(h, "GET", path, nil); w.Code != 404 {
			t.Fatal("invalid ID accepted")
		}
	}
	for _, query := range []string{"page=0", "page=-1", "page=x", "page=1000001", "page=1&page=2", "page=%ZZ", "user_id=2"} {
		if w := requestBook(h, "GET", "/books?"+query, nil); w.Code != 400 {
			t.Fatalf("invalid query accepted: %s", query)
		}
	}
}

func TestBookPagination(t *testing.T) {
	store := newBookStore()
	for i := 0; i < 21; i++ {
		_, _ = store.Create(context.Background(), 1, books.Input{Title: "本", Status: "unread", Notes: ""})
	}
	h, _ := bookApp(t, store, bookAuth{id: 1})
	first := requestBook(h, "GET", "/books", nil)
	if first.Code != 200 || !strings.Contains(first.Body.String(), "/books?page=2") || strings.Count(first.Body.String(), `class="book-title"`) != 20 {
		t.Fatal("first page incorrect")
	}
	second := requestBook(h, "GET", "/books?page=2", nil)
	if second.Code != 200 || !strings.Contains(second.Body.String(), "/books?page=1") || strings.Count(second.Body.String(), `class="book-title"`) != 1 {
		t.Fatal("second page incorrect")
	}
}
