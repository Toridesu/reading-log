package web

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// _test.go内のTestで始まる関数は、go testで実行される。
// tはテストの成否を報告したり、子テストを実行したりするための引数。
func TestRoutes(t *testing.T) {
	// テストケースを構造体のスライス（要素の並び）にして、同じ検証を繰り返す。
	// rangeは要素を順に取り出す。_で添字を捨て、tcで今回のケースを受け取る。
	for _, tc := range []struct {
		method string
		path   string
		status int
		body   string
	}{
		{http.MethodGet, "/", http.StatusOK, "読書ノート"},
		{http.MethodGet, "/healthz", http.StatusOK, `{"status":"ok"}`},
		{http.MethodGet, "/static/app.css", http.StatusOK, "body"},
		{http.MethodGet, "/missing", http.StatusNotFound, "404"},
		{http.MethodPost, "/healthz", http.StatusMethodNotAllowed, "Method Not Allowed"},
	} {
		// ケースごとに名前を付け、どのURLで失敗したのか分かるようにする。
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			// 本番では標準出力へ出すログを、このテストではメモリへ保存して検証する。
			var logs bytes.Buffer
			h, err := NewHandler(slog.New(slog.NewJSONHandler(&logs, nil)))
			if err != nil {
				// Fatalはこの子テストを失敗にして終了する。HTTP処理を続ける必要はない。
				t.Fatal(err)
			}
			// httptestでリクエストと応答先を用意し、実際のポートを開かずにHandlerを試す。
			response := httptest.NewRecorder()
			h.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
			// ステータスと本文が、ケースで指定した期待値に合うか確認する。
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d", response.Code, tc.status)
			}
			if !strings.Contains(response.Body.String(), tc.body) {
				t.Fatalf("unexpected body: %s", response.Body.String())
			}
			// 必要なログ項目だけを読み取るための構造体。
			// `json:"request_id"`のようなタグで、JSONのキーとGoのフィールドを対応付ける。
			var event struct {
				RequestID string `json:"request_id"`
				Status    int    `json:"status"`
			}
			// &eventを渡すと、JSONから読んだ値がこの変数へ書き込まれる。
			if err := json.Unmarshal(logs.Bytes(), &event); err != nil {
				t.Fatalf("invalid JSON log: %v", err)
			}
			// 応答とログが同じ処理を指していることまで確かめる。
			if event.Status != tc.status || event.RequestID == "" || event.RequestID != response.Header().Get("X-Request-ID") {
				t.Fatalf("log does not match response: %+v", event)
			}
		})
	}
}

// TestRequestLogExcludesCredentials は、秘密情報がログに混ざらないことを確認する。
func TestRequestLogExcludesCredentials(t *testing.T) {
	var logs bytes.Buffer
	h, err := NewHandler(slog.New(slog.NewJSONHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	// ダミーの認証コード・トークン・Cookie・本文を意図的にリクエストへ含める。
	// /callbackはまだ未実装だが、共通のログ処理は404応答にも適用される。
	r := httptest.NewRequest(http.MethodPost, "/callback?code=private-code&state=private-state", strings.NewReader("private-body"))
	r.Header.Set("Authorization", "Bearer private-token")
	r.Header.Set("Cookie", "session=private-session")
	h.ServeHTTP(httptest.NewRecorder(), r)
	// Errorfは失敗を記録して処理を続けるので、漏れた項目をまとめて確認できる。
	for _, secret := range []string{"private-code", "private-state", "private-body", "private-token", "private-session"} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("log contains %s", secret)
		}
	}
}
