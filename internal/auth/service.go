// OIDC認証の往復とCookieを扱う。パスワードの照合やトークンの署名検証は既存の仕組みに任せる。
package auth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/time/rate"
)

var ErrUnauthenticated = errors.New("not authenticated")

type Service struct {
	cfg      Config
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	client   *http.Client
	store    Store
	logger   *slog.Logger
	limiter  *rate.Limiter
}

func New(ctx context.Context, cfg Config, store Store, logger *slog.Logger) (*Service, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	ctx = oidc.ClientContext(ctx, client)
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, errors.New("OIDC discovery failed; check issuer URL and Keycloak availability")
	}
	endpoint := provider.Endpoint()
	for _, address := range []string{endpoint.AuthURL, endpoint.TokenURL} {
		if _, err := checkedURL(address); err != nil {
			return nil, errors.New("invalid OIDC endpoint")
		}
	}
	endpoint.AuthStyle = oauth2.AuthStyleInHeader
	return &Service{cfg: cfg, store: store, logger: logger, client: client,
		oauth:    oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURL: cfg.BaseURL + "/auth/callback", Endpoint: endpoint, Scopes: []string{oidc.ScopeOpenID, "profile"}},
		verifier: provider.VerifierContext(ctx, &oidc.Config{ClientID: cfg.ClientID, SupportedSigningAlgs: []string{oidc.RS256}}),
		limiter:  rate.NewLimiter(rate.Every(time.Second), 20)}, nil
}

func (s *Service) cookieName(kind string) string {
	if s.cfg.secureCookies() {
		return "__Host-readinglog_" + kind
	}
	return "readinglog_" + kind
}

func (s *Service) setCookie(w http.ResponseWriter, kind, value string, seconds int) {
	expires := time.Now().Add(time.Duration(seconds) * time.Second)
	if seconds < 0 {
		expires = time.Unix(1, 0)
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(kind), Value: value, Path: "/", MaxAge: seconds, Expires: expires,
		HttpOnly: true, Secure: s.cfg.secureCookies(), SameSite: http.SameSiteLaxMode})
}

func validToken(value string) bool {
	if len(value) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func (s *Service) readCookie(r *http.Request, kind string) string {
	cookies := r.CookiesNamed(s.cookieName(kind))
	if len(cookies) != 1 || !validToken(cookies[0].Value) {
		return ""
	}
	return cookies[0].Value
}

func (s *Service) reject(w http.ResponseWriter, r *http.Request, status int, reason string) {
	// OAuthエラーの本文にはトークン等が含まれ得る。ログは自分で決めた理由コードだけにする。
	s.logger.Warn("auth_rejected", "request_id", w.Header().Get("X-Request-ID"), "reason", reason)
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "認証処理を完了できませんでした。トップ画面からやり直してください。", status)
}

func (s *Service) allowed(w http.ResponseWriter, r *http.Request) bool {
	origin, _ := url.Parse(s.cfg.BaseURL)
	if r.Host != origin.Host {
		s.reject(w, r, http.StatusBadRequest, "invalid_host")
		return false
	}
	if !s.limiter.Allow() {
		w.Header().Set("Retry-After", "1")
		s.reject(w, r, http.StatusTooManyRequests, "rate_limited")
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	return true
}

func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	if !s.allowed(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	state, browser := oauth2.GenerateVerifier(), oauth2.GenerateVerifier()
	flow := LoginRequest{Nonce: oauth2.GenerateVerifier(), Verifier: oauth2.GenerateVerifier()}
	if err := s.store.StartLogin(ctx, state, browser, flow); err != nil {
		s.reject(w, r, 503, "login_store_failed")
		return
	}
	s.setCookie(w, "login", browser, 300)
	// stateは往復の対応、nonceはIDトークンの対応、PKCEは認証コードを交換する相手を確認する。
	target := s.oauth.AuthCodeURL(state, oidc.Nonce(flow.Nonce), oauth2.S256ChallengeOption(flow.Verifier), oauth2.SetAuthURLParam("prompt", "login"))
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Service) Callback(w http.ResponseWriter, r *http.Request) {
	if !s.allowed(w, r) {
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query["state"]) != 1 || !validToken(query.Get("state")) {
		s.reject(w, r, 400, "invalid_state")
		return
	}
	browser := s.readCookie(r, "login")
	if browser == "" {
		s.reject(w, r, 400, "missing_login_cookie")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	flow, err := s.store.ConsumeLogin(ctx, query.Get("state"), browser)
	if errors.Is(err, sql.ErrNoRows) {
		s.reject(w, r, 400, "invalid_or_expired_login")
		return
	}
	if err != nil {
		s.reject(w, r, 503, "login_store_failed")
		return
	}
	s.setCookie(w, "login", "", -1)
	if query.Has("error") || len(query["code"]) != 1 || query.Get("code") == "" {
		s.reject(w, r, 400, "invalid_authorization_response")
		return
	}
	// クライアントシークレットとPKCE verifierをサーバーから送り、認証コードをトークンへ交換する。
	token, err := s.oauth.Exchange(oidc.ClientContext(ctx, s.client), query.Get("code"), oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		s.reject(w, r, 400, "code_exchange_failed")
		return
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		s.reject(w, r, 400, "missing_id_token")
		return
	}
	id, err := s.verifier.Verify(ctx, raw)
	if err != nil {
		s.reject(w, r, 400, "invalid_id_token")
		return
	}
	if id.Subject == "" || id.IssuedAt.IsZero() || subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(flow.Nonce)) != 1 || id.IssuedAt.After(time.Now().Add(time.Minute)) {
		s.reject(w, r, 400, "invalid_identity_claims")
		return
	}
	var claims struct {
		Name            string `json:"name"`
		Username        string `json:"preferred_username"`
		AuthorizedParty string `json:"azp"`
	}
	if err := id.Claims(&claims); err != nil || (len(id.Audience) > 1 && claims.AuthorizedParty == "") || (claims.AuthorizedParty != "" && claims.AuthorizedParty != s.cfg.ClientID) {
		s.reject(w, r, 400, "invalid_authorized_party")
		return
	}
	name := claims.Username
	if name == "" {
		name = claims.Name
	}
	if name == "" || utf8.RuneCountInString(name) > 200 {
		name = "ユーザー"
	}
	sessionToken := oauth2.GenerateVerifier()
	if err := s.store.CreateSession(ctx, Identity{id.Issuer, id.Subject, name}, sessionToken, s.readCookie(r, "session")); err != nil {
		s.reject(w, r, 503, "session_store_failed")
		return
	}
	s.setCookie(w, "session", sessionToken, 8*60*60)
	s.logger.Info("login_succeeded", "request_id", w.Header().Get("X-Request-ID"))
	http.Redirect(w, r, "/books", http.StatusSeeOther)
}

// SessionはDBの期限・失効を毎回確認する。Cookieがあるだけではログイン済みと扱わない。
func (s *Service) Session(r *http.Request) (Session, error) {
	token := s.readCookie(r, "session")
	if token == "" {
		return Session{}, ErrUnauthenticated
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	session, err := s.store.FindSession(ctx, token)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrUnauthenticated
	}
	if err != nil {
		return Session{}, err
	}
	session.CSRFToken = base64.RawURLEncoding.EncodeToString(hash("csrf:" + token))
	return session, nil
}

func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	// POSTと同一origin、セッションに結び付いたCSRFトークンをすべて確認する。
	if !s.CheckHost(r) {
		s.reject(w, r, 400, "invalid_host")
		return
	}
	if r.Header.Get("Origin") != s.cfg.BaseURL {
		s.reject(w, r, 403, "invalid_origin")
		return
	}
	session, err := s.Session(r)
	if errors.Is(err, ErrUnauthenticated) {
		s.setCookie(w, "session", "", -1)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err != nil {
		s.reject(w, r, 503, "session_store_failed")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil || len(r.PostForm["csrf_token"]) != 1 || subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf_token")), []byte(session.CSRFToken)) != 1 {
		s.reject(w, r, 403, "invalid_csrf")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.store.DeleteSession(ctx, s.readCookie(r, "session")); err != nil {
		s.reject(w, r, 503, "session_store_failed")
		return
	}
	s.setCookie(w, "session", "", -1)
	s.setCookie(w, "login", "", -1)
	w.Header().Set("Cache-Control", "no-store")
	s.logger.Info("logout_succeeded", "request_id", w.Header().Get("X-Request-ID"), "user_id", session.UserID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// CheckHostは設定済みのホスト名だけを受け付ける。転送ヘッダーからURLを組み立てない。
func (s *Service) CheckHost(r *http.Request) bool {
	origin, _ := url.Parse(s.cfg.BaseURL)
	return strings.EqualFold(r.Host, origin.Host)
}
