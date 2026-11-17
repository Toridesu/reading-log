// 初回のDBユーザー作成。保存済みのパスワードを再利用し、ローテーションは行わない。
package bootstrap

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"readinglog/internal/database"
)

const CAFile = "/app/certs/ap-northeast-1-bundle.pem"
const lockID int64 = 72656164696

var ErrNoValue = errors.New("secret has no current value")

// テストではAWSの代わりにメモリ上の保存先を使う。
type Secrets interface {
	Read(context.Context, string) (string, error)
	Write(context.Context, string, string, string) error
}

type Config struct {
	Host, Port, RootCert                  string
	MasterSecretARN, ApplicationSecretARN string
}

func (c Config) Validate() error {
	port, err := strconv.Atoi(c.Port)
	if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*$`).MatchString(c.Host) || err != nil || port < 1 || port > 65535 || c.RootCert == "" || c.MasterSecretARN == "" || c.ApplicationSecretARN == "" || c.MasterSecretARN == c.ApplicationSecretARN {
		return errors.New("bootstrap configuration is invalid")
	}
	return nil
}

func (c Config) connectionURL(user, password string) string {
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(c.Host, c.Port), Path: "/readinglog", User: url.UserPassword(user, password)}
	u.RawQuery = url.Values{"sslmode": {"verify-full"}, "sslrootcert": {c.RootCert}, "connect_timeout": {"10"}}.Encode()
	return u.String()
}

func (c Config) applicationPassword(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err == nil && u.User != nil && u.User.Username() == "readinglog" {
		password, ok := u.User.Password()
		// 接続先・TLS・追加パラメーターも含め、この処理が作る形式に限定する。
		if ok && password != "" && !strings.ContainsRune(password, 0) && raw == c.connectionURL("readinglog", password) {
			return password, nil
		}
	}
	return "", errors.New("application secret does not match the expected database URL")
}

func Run(ctx context.Context, c Config, secrets Secrets) error {
	if err := c.Validate(); err != nil {
		return err
	}
	raw, err := secrets.Read(ctx, c.MasterSecretARN)
	if err != nil {
		return errors.New("read administrator secret failed")
	}
	var master struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if json.Unmarshal([]byte(raw), &master) != nil || master.Username != "readinglog_admin" || master.Password == "" {
		return errors.New("administrator secret is invalid")
	}
	admin, err := database.Open(ctx, c.connectionURL(master.Username, master.Password))
	if err != nil {
		return errors.New("administrator database connection failed")
	}
	defer admin.Close()
	// 同じ接続を占有してロックを保持し、同時起動による二重生成を防ぐ。
	conn, err := admin.Conn(ctx)
	if err != nil {
		return errors.New("acquire bootstrap connection failed")
	}
	defer conn.Close()
	var owner bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_get_userbyid(datdba) = current_user FROM pg_database WHERE datname = current_database()`).Scan(&owner); err != nil || !owner {
		return errors.New("bootstrap requires the administrator-owned readinglog database")
	}
	var locked bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", lockID).Scan(&locked); err != nil || !locked {
		return errors.New("bootstrap is already running or lock acquisition failed")
	}
	// プールを閉じてセッションごと解放する。中断時も接続切断でロックが外れる。
	var exists bool
	if err := conn.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'readinglog')").Scan(&exists); err != nil {
		return errors.New("inspect application role failed")
	}
	appURL, err := secrets.Read(ctx, c.ApplicationSecretARN)
	if errors.Is(err, ErrNoValue) {
		if exists {
			return errors.New("application role exists but its secret is missing; recover the secret before retrying")
		}
		password, err := randomHex()
		if err != nil {
			return err
		}
		appURL = c.connectionURL("readinglog", password)
		token, err := randomHex()
		if err != nil {
			return err
		}
		// 先に保存する。DB作成が失敗しても、次回は同じ値で再開できる。
		if err := secrets.Write(ctx, c.ApplicationSecretARN, appURL, token); err != nil {
			return errors.New("save application secret failed; retry without deleting its value")
		}
	} else if err != nil {
		return errors.New("read application secret failed")
	}
	password, err := c.applicationPassword(appURL)
	if err != nil {
		return err
	}
	if exists {
		var safe bool
		err := conn.QueryRowContext(ctx, `SELECT rolcanlogin AND NOT (rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls)
   AND NOT EXISTS (SELECT 1 FROM pg_auth_members WHERE member = pg_roles.oid)
   FROM pg_roles WHERE rolname = 'readinglog'`).Scan(&safe)
		if err != nil || !safe {
			return errors.New("existing application role has unexpected privileges")
		}
		// 既存パスワードを上書きして不一致を隠さない。
		probe, err := database.Open(ctx, appURL)
		if err != nil {
			return errors.New("existing application credentials do not work; no password was changed")
		}
		probe.Close()
	}
	if err := initialize(ctx, conn, exists, password); err != nil {
		return err
	}
	probe, err := database.Open(ctx, appURL)
	if err != nil {
		return errors.New("application connection verification failed; retry with the saved secret")
	}
	defer probe.Close()
	var ready bool
	if err := probe.QueryRowContext(ctx, `SELECT current_user = 'readinglog' AND has_schema_privilege(current_user, 'public', 'USAGE') AND has_schema_privilege(current_user, 'public', 'CREATE')`).Scan(&ready); err != nil || !ready {
		return errors.New("application schema permissions verification failed")
	}
	return nil
}

func initialize(ctx context.Context, conn *sql.Conn, exists bool, password string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("begin database initialization failed")
	}
	defer tx.Rollback()
	if !exists {
		// CREATE ROLEは値のプレースホルダーを使えない。標準文字列に固定して引用符をエスケープする。
		if _, err := tx.ExecContext(ctx, "SET LOCAL standard_conforming_strings = on"); err != nil {
			return errors.New("configure SQL quoting failed")
		}
		if _, err := tx.ExecContext(ctx, "SET LOCAL password_encryption = 'scram-sha-256'"); err != nil {
			return errors.New("configure password encryption failed")
		}
		query := "CREATE ROLE readinglog LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '" + strings.ReplaceAll(password, "'", "''") + "'"
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return errors.New("create application role failed; saved secret can be reused")
		}
	}
	for _, query := range []string{
		"REVOKE ALL ON DATABASE readinglog FROM PUBLIC",
		"GRANT CONNECT ON DATABASE readinglog TO readinglog",
		"REVOKE ALL ON SCHEMA public FROM PUBLIC",
		"GRANT USAGE, CREATE ON SCHEMA public TO readinglog",
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return errors.New("configure application permissions failed; transaction rolled back")
		}
	}
	if err := tx.Commit(); err != nil {
		return errors.New("commit database initialization failed; retry with the saved secret")
	}
	return nil
}

func randomHex() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", errors.New("generate bootstrap credentials failed")
	}
	return hex.EncodeToString(value[:]), nil
}
