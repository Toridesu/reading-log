// 読書画面とフォームの処理。認証・CSRF・入力を確認してからDBへ渡す。
package web

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"html/template"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"readinglog/internal/auth"
	"readinglog/internal/books"
)

type pageData struct {
	Session                 *auth.Session
	PageTitle               string
	Books                   []books.Book
	Book                    books.Book
	Input                   books.Input
	Errors                  map[string]string
	FormAction, SubmitLabel string
	PreviousPage, NextPage  int
	ErrorTitle              string
}

type bookHandler struct {
	logger *slog.Logger
	login  Authentication
	store  books.Store
	pages  *template.Template
}

func (h *bookHandler) render(w http.ResponseWriter, r *http.Request, name string, status int, data pageData) {
	var body bytes.Buffer
	if err := h.pages.ExecuteTemplate(&body, name, data); err != nil {
		h.logger.Error("render_failed", "request_id", w.Header().Get("X-Request-ID"))
		http.Error(w, "画面を表示できませんでした。", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}

// withUserは各操作の入口。Cookieが有効でも、所有者の確認はDBのSQLでさらに行う。
func (h *bookHandler) withUser(next func(http.ResponseWriter, *http.Request, auth.Session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !h.login.CheckHost(r) {
			http.Error(w, "Bad Request", 400)
			return
		}
		session, err := h.login.Session(r)
		if errors.Is(err, auth.ErrUnauthenticated) {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if err != nil || session.UserID <= 0 {
			h.logger.Warn("session_unavailable", "request_id", w.Header().Get("X-Request-ID"))
			h.render(w, r, "error.html", 503, pageData{PageTitle: "一時的に利用できません", ErrorTitle: "一時的に利用できません"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		next(w, r.WithContext(ctx), session)
	}
}

func (h *bookHandler) event(w http.ResponseWriter, session auth.Session, event string, id int64) {
	h.logger.Info(event, "request_id", w.Header().Get("X-Request-ID"), "user_id", session.UserID, "book_id", id)
}

func (h *bookHandler) failure(w http.ResponseWriter, r *http.Request, session auth.Session, id int64, err error) {
	status, title, event := 503, "一時的に利用できません", "book_store_failed"
	if errors.Is(err, books.ErrNotFound) {
		status, title, event = 404, "本が見つかりません", "book_not_found"
	}
	h.event(w, session, event, id)
	h.render(w, r, "error.html", status, pageData{Session: &session, PageTitle: title, ErrorTitle: title})
}

func (h *bookHandler) reject(w http.ResponseWriter, r *http.Request, session auth.Session, status int, reason string) {
	h.logger.Warn("book_request_rejected", "request_id", w.Header().Get("X-Request-ID"), "user_id", session.UserID, "reason", reason)
	h.render(w, r, "error.html", status, pageData{Session: &session, PageTitle: "操作できませんでした", ErrorTitle: "操作できませんでした"})
}

// mutationは変更系フォーム共通の確認。クエリや余分なフィールドから所有者を指定させない。
func (h *bookHandler) mutation(w http.ResponseWriter, r *http.Request, session auth.Session, fields ...string) bool {
	if !h.login.CheckOrigin(r) {
		h.reject(w, r, session, 403, "invalid_origin")
		return false
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/x-www-form-urlencoded" {
		h.reject(w, r, session, 415, "invalid_content_type")
		return false
	}
	// URLエンコードされた日本語・絵文字のメモも上限文字数まで受け取れる大きさにする。
	r.Body = http.MaxBytesReader(w, r.Body, 128*1024)
	if err := r.ParseForm(); err != nil {
		status := 400
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			status = 413
		}
		h.reject(w, r, session, status, "invalid_body")
		return false
	}
	if len(r.PostForm["csrf_token"]) != 1 || session.CSRFToken == "" || subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf_token")), []byte(session.CSRFToken)) != 1 {
		h.reject(w, r, session, 403, "invalid_csrf")
		return false
	}
	allowed := map[string]bool{"csrf_token": true}
	for _, field := range fields {
		allowed[field] = true
	}
	if r.URL.RawQuery != "" {
		h.reject(w, r, session, 400, "unexpected_query")
		return false
	}
	for key, values := range r.PostForm {
		if !allowed[key] || len(values) != 1 {
			h.reject(w, r, session, 400, "unexpected_field")
			return false
		}
	}
	return true
}

func bookID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, books.ErrNotFound
	}
	return id, nil
}

func (h *bookHandler) list(w http.ResponseWriter, r *http.Request, session auth.Session) {
	page := 1
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		h.reject(w, r, session, 400, "invalid_page")
		return
	}
	for key := range query {
		if key != "page" {
			h.reject(w, r, session, 400, "unexpected_query")
			return
		}
	}
	if values, ok := query["page"]; ok {
		if len(values) != 1 {
			h.reject(w, r, session, 400, "invalid_page")
			return
		}
		page, err = strconv.Atoi(values[0])
	}
	if err != nil || page < 1 || page > 1000000 {
		h.reject(w, r, session, 400, "invalid_page")
		return
	}
	list, more, err := h.store.List(r.Context(), session.UserID, page)
	if err != nil {
		h.failure(w, r, session, 0, err)
		return
	}
	data := pageData{Session: &session, PageTitle: "本棚", Books: list}
	if page > 1 {
		data.PreviousPage = page - 1
	}
	if more {
		data.NextPage = page + 1
	}
	h.render(w, r, "books.html", 200, data)
}

func (h *bookHandler) new(w http.ResponseWriter, r *http.Request, session auth.Session) {
	h.render(w, r, "book-form.html", 200, pageData{Session: &session, PageTitle: "本を追加", FormAction: "/books", SubmitLabel: "追加", Input: books.Input{Status: "unread"}})
}

func (h *bookHandler) show(name string) func(http.ResponseWriter, *http.Request, auth.Session) {
	return func(w http.ResponseWriter, r *http.Request, session auth.Session) {
		id, err := bookID(r)
		if err != nil {
			h.failure(w, r, session, 0, err)
			return
		}
		book, err := h.store.Get(r.Context(), session.UserID, id)
		if err != nil {
			h.failure(w, r, session, id, err)
			return
		}
		data := pageData{Session: &session, PageTitle: book.Title, Book: book, Input: books.Input{Title: book.Title, Status: book.Status, Notes: book.Notes}, FormAction: "/books/" + strconv.FormatInt(id, 10), SubmitLabel: "保存"}
		if name == "book-form.html" {
			data.PageTitle = "本を編集"
		}
		if name == "book-delete.html" {
			data.PageTitle = "本を削除"
		}
		h.render(w, r, name, 200, data)
	}
}

func (h *bookHandler) save(create bool) func(http.ResponseWriter, *http.Request, auth.Session) {
	return func(w http.ResponseWriter, r *http.Request, session auth.Session) {
		if !h.mutation(w, r, session, "title", "status", "notes") {
			return
		}
		var id int64
		if !create {
			var err error
			id, err = bookID(r)
			if err != nil {
				h.failure(w, r, session, 0, err)
				return
			}
			// 入力エラーの再表示でも、他人のタスクの編集画面を返さない。
			if _, err := h.store.Get(r.Context(), session.UserID, id); err != nil {
				h.failure(w, r, session, id, err)
				return
			}
		}
		input, problems := books.Validate(books.Input{Title: r.PostForm.Get("title"), Status: r.PostForm.Get("status"), Notes: r.PostForm.Get("notes")})
		if len(problems) > 0 {
			data := pageData{Session: &session, PageTitle: "本を編集", Book: books.Book{ID: id}, Input: input, Errors: problems, FormAction: "/books/" + strconv.FormatInt(id, 10), SubmitLabel: "保存"}
			if create {
				data.PageTitle, data.FormAction, data.SubmitLabel = "本を追加", "/books", "追加"
			}
			h.render(w, r, "book-form.html", 422, data)
			return
		}
		var err error
		event := "book_updated"
		if create {
			id, err = h.store.Create(r.Context(), session.UserID, input)
			event = "book_created"
		} else {
			err = h.store.Update(r.Context(), session.UserID, id, input)
		}
		if err != nil {
			h.failure(w, r, session, id, err)
			return
		}
		h.event(w, session, event, id)
		http.Redirect(w, r, "/books/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
	}
}

func (h *bookHandler) delete(w http.ResponseWriter, r *http.Request, session auth.Session) {
	if !h.mutation(w, r, session) {
		return
	}
	id, err := bookID(r)
	if err != nil {
		h.failure(w, r, session, 0, err)
		return
	}
	if err := h.store.Delete(r.Context(), session.UserID, id); err != nil {
		h.failure(w, r, session, id, err)
		return
	}
	h.event(w, session, "book_deleted", id)
	http.Redirect(w, r, "/books", http.StatusSeeOther)
}
