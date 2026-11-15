// 接続先を変えてもissuer・署名・nonceの確認が残り、HTTPS環境では開発用設定を使えないことを検証する。
package auth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestLocalOIDCValidation(t *testing.T) {
	for _, mode := range []string{"success", "backend issuer", "bad signature", "wrong nonce"} {
		t.Run(mode, func(t *testing.T) {
			f := localFixture(t, true)
			switch mode {
			case "backend issuer":
				f.override["iss"] = f.idp.URL
			case "bad signature":
				f.mode = "bad_signature"
			case "wrong nonce":
				f.override["nonce"] = "wrong"
			}
			state, cookie := f.start(t)
			result := f.callback(state, cookie)
			if mode == "success" {
				if result.Code != 303 || len(f.store.sessions) != 1 {
					t.Fatal("mapped OIDC login failed")
				}
			} else if result.Code != 400 || len(f.store.sessions) != 0 {
				t.Fatal("mapped transport bypassed token validation")
			}
		})
	}
}

func TestLocalAddressConfig(t *testing.T) {
	base := Config{BaseURL: "http://localhost:8080", Issuer: "http://localhost:8081/realms/reading-log", ClientID: "app", ClientSecret: "test", LocalAddress: "keycloak:8080"}
	for _, address := range []string{"", "keycloak:8080", "127.0.0.1:1234", "[::1]:8080"} {
		cfg := base
		cfg.LocalAddress = address
		if cfg.validate() != nil {
			t.Fatalf("valid local address rejected: %s", address)
		}
	}
	for _, address := range []string{"keycloak", "https://keycloak:8080", "keycloak:0", "keycloak:65536", "keycloak:abc", "idp.example:8080", "169.254.169.254:80", ":8080"} {
		cfg := base
		cfg.LocalAddress = address
		if cfg.validate() == nil {
			t.Fatal("invalid local address accepted")
		}
	}
	for _, field := range []string{"base", "issuer"} {
		cfg := base
		if field == "base" {
			cfg.BaseURL = "https://reading.example"
		} else {
			cfg.Issuer = "https://auth.example"
		}
		if cfg.validate() == nil {
			t.Fatal("local override allowed in HTTPS environment")
		}
	}
}

func TestLocalTransportKeepsHostAndOtherDestinations(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Host)
	}))
	defer target.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "other server")
	}))
	defer other.Close()
	u, _ := url.Parse(target.URL)
	client := oidcClient(Config{Issuer: "http://localhost:1", LocalAddress: u.Host})
	defer client.CloseIdleConnections()
	for address, expected := range map[string]string{"http://localhost:1/keys": "localhost:1", other.URL: "other server"} {
		response, err := client.Get(address)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || string(body) != expected {
			t.Fatal("wrong host or unintended destination replacement")
		}
	}
}
