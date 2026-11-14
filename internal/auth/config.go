// 認証に使うURLとクライアント情報を読み込む。HTTPはローカル開発だけに限定する。
package auth

import (
	"errors"
	"net"
	"net/url"
	"os"
)

type Config struct{ BaseURL, Issuer, ClientID, ClientSecret string }

func FromEnv() Config {
	return Config{os.Getenv("APP_BASE_URL"), os.Getenv("OIDC_ISSUER"), os.Getenv("OIDC_CLIENT_ID"), os.Getenv("OIDC_CLIENT_SECRET")}
}

func (c Config) validate() error {
	if c.ClientID == "" || c.ClientSecret == "" {
		return errors.New("OIDC client ID and secret are required")
	}
	base, err := checkedURL(c.BaseURL)
	if err != nil || base.Path != "" {
		return errors.New("APP_BASE_URL must be an origin, without a path or trailing slash")
	}
	if _, err := checkedURL(c.Issuer); err != nil {
		return errors.New("OIDC_ISSUER must be HTTPS, or HTTP on localhost")
	}
	return nil
}

func checkedURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid URL")
	}
	if u.Scheme == "https" {
		return u, nil
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme == "http" && (u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())) {
		return u, nil
	}
	return nil, errors.New("HTTPS is required outside localhost")
}

func (c Config) secureCookies() bool {
	u, err := url.Parse(c.BaseURL)
	return err == nil && u.Scheme == "https"
}
