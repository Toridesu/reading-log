package web

import (
	"context"
	"crypto/rand"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"readinglog/internal/auth"
	"readinglog/internal/books"
)

// HTMLとCSSを実行ファイルへ埋め込む。
//
//go:embed templates/*.html static/*
var assets embed.FS

// Authenticationは画面から利用する認証処理。テストではKeycloakなしで画面の振る舞いを確認する。
type Authentication interface {
	Login(http.ResponseWriter, *http.Request)
	Callback(http.ResponseWriter, *http.Request)
	Logout(http.ResponseWriter, *http.Request)
	Session(*http.Request) (auth.Session, error)
	CheckHost(*http.Request) bool
	CheckOrigin(*http.Request) bool
}

// NewHandler はURLごとの処理と共通のリクエストログを組み立てる。
func NewHandler(logger *slog.Logger, pingDB func(context.Context) error, login Authentication, store books.Store) (http.Handler, error) {
	if pingDB == nil || login == nil || store == nil {
		return nil, errors.New("database health check, authentication and book store are required")
	}
	pages, err := template.ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	static, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	h := &bookHandler{logger: logger, login: login, store: store, pages: pages}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !login.CheckHost(r) {
			http.Error(w, "Bad Request", 400)
			return
		}
		_, err := login.Session(r)
		if errors.Is(err, auth.ErrUnauthenticated) {
			h.render(w, r, "home.html", 200, pageData{PageTitle: "読書ノート"})
			return
		}
		if err != nil {
			logger.Warn("session_unavailable", "request_id", w.Header().Get("X-Request-ID"))
			h.render(w, r, "error.html", 503, pageData{PageTitle: "一時的に利用できません", ErrorTitle: "一時的に利用できません"})
			return
		}
		http.Redirect(w, r, "/books", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /books", h.withUser(h.list))
	mux.HandleFunc("GET /books/new", h.withUser(h.new))
	mux.HandleFunc("POST /books", h.withUser(h.save(true)))
	mux.HandleFunc("GET /books/{id}", h.withUser(h.show("book.html")))
	mux.HandleFunc("GET /books/{id}/edit", h.withUser(h.show("book-form.html")))
	mux.HandleFunc("POST /books/{id}", h.withUser(h.save(false)))
	mux.HandleFunc("GET /books/{id}/delete", h.withUser(h.show("book-delete.html")))
	mux.HandleFunc("POST /books/{id}/delete", h.withUser(h.delete))
	mux.HandleFunc("GET /auth/login", login.Login)
	mux.HandleFunc("GET /auth/callback", login.Callback)
	mux.HandleFunc("POST /auth/logout", login.Logout)
	// /healthz はアプリの稼働確認、/readyz はDBへの接続確認。
	// DBだけが止まった場合と、アプリ自体が止まった場合を切り分ける。
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if err := pingDB(ctx); err != nil {
			logger.Warn("database_unavailable", "request_id", w.Header().Get("X-Request-ID"))
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("{\"status\":\"unavailable\"}\n"))
			return
		}
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	return requestLog(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 認証コードがURLに含まれるコールバックでも、外部へRefererを送らない。
		w.Header().Set("Referrer-Policy", "same-origin")
		if strings.HasPrefix(r.URL.Path, "/auth/") {
			w.Header().Set("Referrer-Policy", "no-referrer")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		mux.ServeHTTP(w, r)
	})), nil
}

// requestLog は各リクエストにIDを付け、処理後のステータスと所要時間を記録する。
func requestLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := rand.Text()
		w.Header().Set("X-Request-ID", requestID)
		recorder := &responseWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		// 認証情報を漏らさないよう、クエリ文字列・Cookie・本文は記録しない。
		logger.Info("http_request",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}

// responseWriter は本来のWriterへ応答を渡しつつ、ログ用のステータスを控える。
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// ステータス指定なしで本文を書いた場合は、通常のWriterと同じく200にする。
func (w *responseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}
