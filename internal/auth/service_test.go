// テスト用の認証サーバーで署名済みIDトークンを発行し、不正な応答を拒否できるか確かめる。
package auth

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

type memoryFlow struct {
	LoginRequest
	expires time.Time
}
type memoryStore struct {
	flows    map[string]memoryFlow
	sessions map[string]Session
	fail     bool
}

func newMemoryStore() *memoryStore {
	return &memoryStore{flows: map[string]memoryFlow{}, sessions: map[string]Session{}}
}
func (m *memoryStore) StartLogin(_ context.Context, state, browser string, f LoginRequest) error {
	if m.fail {
		return errors.New("private-db-password")
	}
	m.flows[state+browser] = memoryFlow{f, time.Now().Add(5 * time.Minute)}
	return nil
}
func (m *memoryStore) ConsumeLogin(_ context.Context, state, browser string) (LoginRequest, error) {
	if m.fail {
		return LoginRequest{}, errors.New("private-db-password")
	}
	f, ok := m.flows[state+browser]
	if !ok || !f.expires.After(time.Now()) {
		return LoginRequest{}, sql.ErrNoRows
	}
	delete(m.flows, state+browser)
	return f.LoginRequest, nil
}
func (m *memoryStore) CreateSession(_ context.Context, i Identity, token, old string) error {
	if m.fail {
		return errors.New("private-db-password")
	}
	delete(m.sessions, old)
	m.sessions[token] = Session{UserID: 1, Name: i.Name, ExpiresAt: time.Now().Add(8 * time.Hour)}
	return nil
}
func (m *memoryStore) FindSession(_ context.Context, token string) (Session, error) {
	if m.fail {
		return Session{}, errors.New("private-db-password")
	}
	s, ok := m.sessions[token]
	if !ok || !s.ExpiresAt.After(time.Now()) {
		return Session{}, sql.ErrNoRows
	}
	return s, nil
}
func (m *memoryStore) DeleteSession(_ context.Context, token string) error {
	if m.fail {
		return errors.New("private-db-password")
	}
	delete(m.sessions, token)
	return nil
}

type authFixture struct {
	service                *Service
	store                  *memoryStore
	logs                   bytes.Buffer
	idp                    *httptest.Server
	key                    *rsa.PrivateKey
	nonce, challenge, mode string
	override               map[string]any
	exchanges              int
}

func fixture(t *testing.T) *authFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &authFixture{store: newMemoryStore(), key: key, override: map[string]any{}}
	mux := http.NewServeMux()
	f.idp = httptest.NewServer(mux)
	t.Cleanup(f.idp.Close)
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": f.idp.URL, "authorization_endpoint": f.idp.URL + "/authorize", "token_endpoint": f.idp.URL + "/token", "jwks_uri": f.idp.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{"kty": "RSA", "kid": "test", "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()), "e": "AQAB"}}})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		f.exchanges++
		client, secret, ok := r.BasicAuth()
		_ = r.ParseForm()
		if !ok || client != "reading-log" || secret != "test-client-secret" || r.Form.Get("code") != "one-use-code" || oauth2.S256ChallengeFromVerifier(r.Form.Get("code_verifier")) != f.challenge || r.Form.Get("redirect_uri") != "http://localhost:8080/auth/callback" {
			t.Error("code exchange did not use expected client, redirect URI and PKCE verifier")
			http.Error(w, "invalid_grant", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if f.mode == "exchange_error" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"private-id-token"}`))
			return
		}
		claims := map[string]any{"iss": f.idp.URL, "sub": "user-a", "aud": "reading-log", "azp": "reading-log", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": f.nonce, "preferred_username": "alice"}
		for k, v := range f.override {
			claims[k] = v
		}
		result := map[string]any{"access_token": "private-access-token", "token_type": "Bearer", "expires_in": 3600}
		if f.mode != "missing_id_token" {
			result["id_token"] = signedToken(t, key, claims, f.mode == "bad_signature")
		}
		_ = json.NewEncoder(w).Encode(result)
	})
	f.service, err = New(context.Background(), Config{"http://localhost:8080", f.idp.URL, "reading-log", "test-client-secret"}, f.store, slog.New(slog.NewJSONHandler(&f.logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func signedToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any, corrupt bool) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test"}`))
	payload, _ := json.Marshal(claims)
	body := header + "." + base64.RawURLEncoding.EncodeToString(payload)
	sum := sha256.Sum256([]byte(body))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	if corrupt {
		sig[0] ^= 1
	}
	return body + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (f *authFixture) start(t *testing.T) (string, *http.Cookie) {
	t.Helper()
	w := httptest.NewRecorder()
	f.service.Login(w, httptest.NewRequest("GET", "http://localhost:8080/auth/login", nil))
	if w.Code != 302 {
		t.Fatalf("login status=%d", w.Code)
	}
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("nonce") == "" || q.Get("prompt") != "login" {
		t.Fatal("missing OIDC protection")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing login cookie")
	}
	if !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].MaxAge != 300 {
		t.Fatal("incorrect cookie attributes")
	}
	f.nonce = q.Get("nonce")
	f.challenge = q.Get("code_challenge")
	return q.Get("state"), cookies[0]
}

func (f *authFixture) callback(state string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "http://localhost:8080/auth/callback?"+url.Values{"state": {state}, "code": {"one-use-code"}}.Encode(), nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	f.service.Callback(w, r)
	return w
}

func TestLoginValidation(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		claims     map[string]any
		mutate     string
	}{
		{name: "success"},
		{name: "wrong state", mutate: "state"}, {name: "missing browser cookie", mutate: "missing_cookie"}, {name: "wrong browser", mutate: "browser"}, {name: "expired login", mutate: "expired"},
		{name: "bad signature", mode: "bad_signature"}, {name: "missing id token", mode: "missing_id_token"}, {name: "exchange failed", mode: "exchange_error"},
		{name: "wrong issuer", claims: map[string]any{"iss": "https://other.example"}},
		{name: "wrong audience", claims: map[string]any{"aud": "another-app"}},
		{name: "expired token", claims: map[string]any{"exp": time.Now().Add(-time.Hour).Unix()}},
		{name: "wrong nonce", claims: map[string]any{"nonce": "wrong"}},
		{name: "missing nonce", claims: map[string]any{"nonce": ""}},
		{name: "missing issued at", claims: map[string]any{"iat": nil}},
		{name: "missing subject", claims: map[string]any{"sub": ""}},
		{name: "future issued at", claims: map[string]any{"iat": time.Now().Add(time.Hour).Unix()}},
		{name: "wrong authorized party", claims: map[string]any{"azp": "another-app"}},
		{name: "multi audience without authorized party", claims: map[string]any{"aud": []string{"reading-log", "other"}, "azp": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture(t)
			f.mode = tc.mode
			if tc.claims != nil {
				f.override = tc.claims
			}
			state, cookie := f.start(t)
			switch tc.mutate {
			case "state":
				state = oauth2.GenerateVerifier()
			case "missing_cookie":
				cookie = nil
			case "browser":
				cookie.Value = oauth2.GenerateVerifier()
			case "expired":
				flow := f.store.flows[state+cookie.Value]
				flow.expires = time.Now().Add(-time.Minute)
				f.store.flows[state+cookie.Value] = flow
			}
			w := f.callback(state, cookie)
			if tc.name == "success" {
				if w.Code != 303 || w.Header().Get("Location") != "/books" || len(f.store.sessions) != 1 {
					t.Fatal("login failed")
				}
				if f.callback(state, cookie).Code != 400 || f.exchanges != 1 {
					t.Fatal("callback replay accepted")
				}
			} else {
				if w.Code != 400 || len(f.store.sessions) != 0 {
					t.Fatalf("invalid login accepted: %d", w.Code)
				}
				if tc.mutate != "" && f.exchanges != 0 {
					t.Fatal("invalid flow reached token endpoint")
				}
			}
			for _, secret := range []string{"one-use-code", state, f.nonce, "test-client-secret", "private-access-token", "private-id-token"} {
				if strings.Contains(w.Body.String()+f.logs.String(), secret) {
					t.Fatal("authentication secret leaked")
				}
			}
		})
	}
}

func TestSessionAndLogout(t *testing.T) {
	f := fixture(t)
	state, flowCookie := f.start(t)
	callback := f.callback(state, flowCookie)
	var cookie *http.Cookie
	for _, c := range callback.Result().Cookies() {
		if c.Name == "readinglog_session" {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.MaxAge != 28800 {
		t.Fatal("session cookie missing")
	}
	request := func(method, path, body string) *http.Request {
		r := httptest.NewRequest(method, "http://localhost:8080"+path, strings.NewReader(body))
		r.AddCookie(cookie)
		return r
	}
	session, err := f.service.Session(request("GET", "/books", ""))
	if err != nil || session.Name != "alice" {
		t.Fatal("session not resolved")
	}
	for _, tc := range []struct{ name, origin, csrf string }{
		{"wrong origin", "https://evil.example", session.CSRFToken}, {"missing origin", "", session.CSRFToken}, {"missing csrf", f.service.cfg.BaseURL, ""}, {"wrong csrf", f.service.cfg.BaseURL, "wrong"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := request("POST", "/auth/logout", url.Values{"csrf_token": {tc.csrf}}.Encode())
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			f.service.Logout(w, r)
			if w.Code != 403 || len(f.store.sessions) != 1 {
				t.Fatal("invalid logout accepted")
			}
		})
	}
	r := request("POST", "/auth/logout", url.Values{"csrf_token": {session.CSRFToken}}.Encode())
	r.Header.Set("Origin", f.service.cfg.BaseURL)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.service.Logout(w, r)
	if w.Code != 303 {
		t.Fatal("logout failed")
	}
	if _, err := f.service.Session(request("GET", "/books", "")); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("logged-out cookie still works")
	}
	f.store.sessions[cookie.Value] = Session{ExpiresAt: time.Now().Add(-time.Minute)}
	if _, err := f.service.Session(request("GET", "/books", "")); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("expired session accepted")
	}
	f.store.fail = true
	if _, err := f.service.Session(request("GET", "/books", "")); err == nil || errors.Is(err, ErrUnauthenticated) {
		t.Fatal("DB outage hidden as guest")
	}
}

func TestSecureCookiesAndConfig(t *testing.T) {
	f := fixture(t)
	f.service.cfg.BaseURL = "https://reading.example"
	w := httptest.NewRecorder()
	f.service.setCookie(w, "session", oauth2.GenerateVerifier(), 28800)
	c := w.Result().Cookies()[0]
	if c.Name != "__Host-readinglog_session" || !c.Secure || !c.HttpOnly || c.Path != "/" || c.Domain != "" {
		t.Fatal("insecure production cookie")
	}
	for _, base := range []string{"http://reading.example", "http://localhost.evil.example", "https://reading.example/extra", "https://user:pass@reading.example", "https://reading.example?x=1"} {
		cfg := f.service.cfg
		cfg.BaseURL = base
		if cfg.validate() == nil {
			t.Errorf("invalid origin accepted: %s", base)
		}
	}
}

func TestAuthErrorsAndRateLimit(t *testing.T) {
	f := fixture(t)
	f.store.fail = true
	w := httptest.NewRecorder()
	f.service.Login(w, httptest.NewRequest("GET", "http://localhost:8080/auth/login", nil))
	if w.Code != 503 || strings.Contains(w.Body.String()+f.logs.String(), "private-db-password") {
		t.Fatal("store failure mishandled")
	}
	f.store.fail = false
	w = httptest.NewRecorder()
	f.service.Login(w, httptest.NewRequest("GET", "http://evil.example/auth/login", nil))
	if w.Code != 400 {
		t.Fatal("untrusted host accepted")
	}
	for i := 0; i < 25; i++ {
		w = httptest.NewRecorder()
		f.service.Login(w, httptest.NewRequest("GET", "http://localhost:8080/auth/login", nil))
	}
	if w.Code != 429 {
		t.Fatal("login not rate limited")
	}
}
