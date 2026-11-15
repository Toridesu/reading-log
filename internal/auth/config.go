// 認証に使うURLとクライアント情報を読み込む。HTTPはローカル開発だけに限定する。
package auth

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
)

type Config struct {
	BaseURL, Issuer, ClientID, ClientSecret string
	LocalAddress                            string
}

func FromEnv() Config {
	return Config{BaseURL: os.Getenv("APP_BASE_URL"), Issuer: os.Getenv("OIDC_ISSUER"), ClientID: os.Getenv("OIDC_CLIENT_ID"), ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"), LocalAddress: os.Getenv("OIDC_LOCAL_ADDRESS")}
}

func (c Config) validate() error {
	if c.ClientID == "" || c.ClientSecret == "" {
		return errors.New("OIDC client ID and secret are required")
	}
	base, err := checkedURL(c.BaseURL)
	if err != nil || base.Path != "" {
		return errors.New("APP_BASE_URL must be an origin, without a path or trailing slash")
	}
	issuer, err := checkedURL(c.Issuer)
	if err != nil {
		return errors.New("OIDC_ISSUER must be HTTPS, or HTTP on localhost")
	}
	if c.LocalAddress != "" {
		// 接続先の置換はローカルHTTP開発だけ。AWSなどのHTTPS構成では起動エラーにする。
		host, port, err := net.SplitHostPort(c.LocalAddress)
		number, portErr := strconv.Atoi(port)
		ip := net.ParseIP(host)
		if base.Scheme != "http" || issuer.Scheme != "http" || err != nil || portErr != nil || number < 1 || number > 65535 || (host != "keycloak" && (ip == nil || !ip.IsLoopback())) {
			return errors.New("OIDC_LOCAL_ADDRESS requires local HTTP URLs and keycloak or a loopback IP with a valid port")
		}
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
