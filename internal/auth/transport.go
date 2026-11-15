// ローカルのコンテナからKeycloakへ接続する補助。issuerやHTTPのHostは変えず、TCPの接続先だけを変える。
package auth

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"time"
)

func oidcClient(cfg Config) *http.Client {
	client := &http.Client{Timeout: 5 * time.Second}
	if cfg.LocalAddress == "" {
		return client
	}
	// cfg.validate済みなので、ここに来るのはlocalhostのHTTP開発時だけ。
	issuer, _ := url.Parse(cfg.Issuer)
	port := issuer.Port()
	if port == "" {
		port = "80"
	}
	source := net.JoinHostPort(issuer.Hostname(), port)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == source {
			address = cfg.LocalAddress
		}
		return dialer.DialContext(ctx, network, address)
	}
	client.Transport = transport
	return client
}
