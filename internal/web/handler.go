package web

import (
	"bytes"
	"crypto/rand"
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"time"
)

// HTMLとCSSを実行ファイルへ埋め込む。
//
//go:embed templates/*.html static/*
var assets embed.FS

// NewHandler はURLごとの処理と共通のリクエストログを組み立てる。
func NewHandler(logger *slog.Logger) (http.Handler, error) {
	pages, err := template.ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	static, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	// /{$} はルートURLへの完全一致。未知のURLまで拾わない。
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		// 描画に失敗した場合、途中のHTMLを送らないよう先にメモリ上で完成させる。
		var body bytes.Buffer
		if err := pages.ExecuteTemplate(&body, "home.html", nil); err != nil {
			logger.Error("render_failed", "request_id", w.Header().Get("X-Request-ID"), "error", err.Error())
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(body.Bytes())
	})
	// HTTPサーバーの稼働確認。DBの接続確認はまだ含まない。
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	return requestLog(logger, mux), nil
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
