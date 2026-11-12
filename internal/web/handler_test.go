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

// TestRoutes は各URLの応答と、それに対応するログを確認する。
func TestRoutes(t *testing.T) {
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
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var logs bytes.Buffer
			h, err := NewHandler(slog.New(slog.NewJSONHandler(&logs, nil)))
			if err != nil {
				t.Fatal(err)
			}
			// 実際のポートを開かず、リクエストをHandlerへ直接渡して検証する。
			response := httptest.NewRecorder()
			h.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d", response.Code, tc.status)
			}
			if !strings.Contains(response.Body.String(), tc.body) {
				t.Fatalf("unexpected body: %s", response.Body.String())
			}
			var event struct {
				RequestID string `json:"request_id"`
				Status    int    `json:"status"`
			}
			if err := json.Unmarshal(logs.Bytes(), &event); err != nil {
				t.Fatalf("invalid JSON log: %v", err)
			}
			if event.Status != tc.status || event.RequestID == "" || event.RequestID != response.Header().Get("X-Request-ID") {
				t.Fatalf("log does not match response: %+v", event)
			}
		})
	}
}

// TestRequestLogExcludesCredentials は、ダミーの認証情報がログに残らないことを確認する。
func TestRequestLogExcludesCredentials(t *testing.T) {
	var logs bytes.Buffer
	h, err := NewHandler(slog.New(slog.NewJSONHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/callback?code=private-code&state=private-state", strings.NewReader("private-body"))
	r.Header.Set("Authorization", "Bearer private-token")
	r.Header.Set("Cookie", "session=private-session")
	h.ServeHTTP(httptest.NewRecorder(), r)
	for _, secret := range []string{"private-code", "private-state", "private-body", "private-token", "private-session"} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("log contains %s", secret)
		}
	}
}
