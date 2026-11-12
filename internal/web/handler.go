// package web に、HTTPで受けたリクエストを処理する機能をまとめる。
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

// HTMLとCSSをコンパイル時に実行ファイルへ埋め込む。
// 起動時に、別の場所からHTML/CSSファイルを探す必要がなくなる。
// 次のgo:embedは通常の説明コメントではなく、Goへの指示なので書式を保つ。
//
//go:embed templates/*.html static/*
var assets embed.FS

// NewHandler はURLごとの処理を組み立て、HTTPサーバーへ渡す。
// 大文字で始まる関数は、別パッケージ（ここではcmd/web）から呼び出せる。
// http.HandlerはServeHTTPメソッドを持つ型が満たすinterface（振る舞いの約束）。
// 戻り値は「完成したHandler」と「準備中のエラー」の2つ。
func NewHandler(logger *slog.Logger) (http.Handler, error) {
	// HTMLをテンプレートとして読み込む。将来データを渡して画面に表示できる。
	// html/templateは、値をHTMLへ埋め込むときに文脈に応じたエスケープを行う。
	pages, err := template.ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	// 埋め込んだファイルのうち、staticフォルダ内を配信用のルートにする。
	static, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}

	// muxは、HTTPメソッドとURLに応じて処理を振り分けるルーター。
	mux := http.NewServeMux()
	// /{$} はルートURL「/」への完全一致を表し、存在しないURLまで拾わない。
	// 引数のfuncは名前のない関数。wへ応答を書き、rからリクエスト情報を読む。
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		// 一度メモリ上でHTMLを完成させ、描画失敗時に途中のHTMLを送らないようにする。
		var body bytes.Buffer
		// &bodyを渡すことで、テンプレートの出力先としてこのバッファを使う。
		// 最後のnilは、今は画面へ渡すデータがないことを表す。
		if err := pages.ExecuteTemplate(&body, "home.html", nil); err != nil {
			logger.Error("render_failed", "request_id", w.Header().Get("X-Request-ID"), "error", err.Error())
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Writeは「書いたバイト数」と「エラー」を返すが、ここでは両方とも使わない。
		// 書き込み中にクライアントが切断しても、別のHTTP応答を送り直すことはしない。
		_, _ = w.Write(body.Bytes())
	})
	// 稼働確認用のURL。現時点ではDBなどの接続状態までは確認しない。
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 古い正常応答がキャッシュから返らないようにする。
		w.Header().Set("Cache-Control", "no-store")
		// []byte(...) は文字列を、Writeが受け取るバイト列へ変換する書き方。
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	})
	// /static/app.cssから/static/を外し、埋め込んだapp.cssを配信する。
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	// ルーター全体をログ処理で包む。未登録URLの404なども記録できる。
	return requestLog(logger, mux), nil
}

// requestLog は、次のHTTP処理の前後に共通処理を追加する「ミドルウェア」。
// リクエスト → ID発行 → nextの処理 → 結果のログ、の順に進む。
func requestLog(logger *slog.Logger, next http.Handler) http.Handler {
	// http.HandlerFuncで、通常の関数をhttp.Handlerとして使える形にする。
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		// リクエストごとにIDを生成し、レスポンスとログを結び付ける。
		requestID := rand.Text()
		w.Header().Set("X-Request-ID", requestID)
		// 本来のWriterを包み、送られたHTTPステータスも記録できるようにする。
		recorder := &responseWriter{ResponseWriter: w}
		// 呼び出しはここで順番に実行される。goroutineを新しく作る処理ではない。
		next.ServeHTTP(recorder, r)
		status := recorder.status
		// 何も書き込まなかった場合も、通常のHTTP応答は200になる。
		if status == 0 {
			status = http.StatusOK
		}
		// クエリ文字列・Cookie・本文には認証情報やユーザーデータが入り得るため記録しない。
		// slogには「項目名, 値」を並べると、JSONのフィールドとして出力される。
		logger.Info("http_request",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}

// responseWriter は、HTTP応答を返しながらステータスを控える構造体。
// http.ResponseWriterを埋め込むことで、Header等のメソッドをそのまま利用できる。
// WriteとWriteHeaderだけは、下の同名メソッドで記録処理を追加する。
type responseWriter struct {
	http.ResponseWriter
	status int
}

// 関数名の前の(w *responseWriter)は「レシーバー」。この型に属するメソッドを定義する。
// ポインタで受け取るので、w.statusの更新が呼び出し元の構造体に反映される。
func (w *responseWriter) WriteHeader(status int) {
	// このアプリでは最初に送ったステータスを保持し、二重送信を防ぐ。
	if w.status != 0 {
		return
	}
	w.status = status
	// 記録しただけでは応答されないので、本来のWriterにも処理を渡す。
	w.ResponseWriter.WriteHeader(status)
}

// WriteHeaderを省略して本文を書く場合も、通常のWriterと同じく200として扱う。
func (w *responseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}
