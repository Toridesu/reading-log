// ログイン途中の情報と、ログイン後のセッションをPostgreSQLへ保存する。
package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"time"
)

type Identity struct{ Issuer, Subject, Name string }
type Session struct {
	UserID          int64
	Name, CSRFToken string
	ExpiresAt       time.Time
}
type LoginRequest struct{ Nonce, Verifier string }

// Storeは認証処理が必要とするDB操作。テストでは差し替えて拒否パターンを再現する。
type Store interface {
	StartLogin(context.Context, string, string, LoginRequest) error
	ConsumeLogin(context.Context, string, string) (LoginRequest, error)
	CreateSession(context.Context, Identity, string, string) error
	FindSession(context.Context, string) (Session, error)
	DeleteSession(context.Context, string) error
}

type PostgresStore struct{ DB *sql.DB }

func hash(value string) []byte { h := sha256.Sum256([]byte(value)); return h[:] }

func (s PostgresStore) StartLogin(ctx context.Context, state, browser string, flow LoginRequest) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 期限を過ぎた情報はログイン開始時に掃除する。有効期限の判定自体は各SQLでも行う。
	for _, query := range []string{"DELETE FROM login_requests WHERE expires_at <= now()", "DELETE FROM sessions WHERE expires_at <= now()"} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO login_requests (state_hash, browser_hash, nonce, verifier, expires_at)
 VALUES ($1,$2,$3,$4,now()+interval '5 minutes')`, hash(state), hash(browser), flow.Nonce, flow.Verifier)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s PostgresStore) ConsumeLogin(ctx context.Context, state, browser string) (LoginRequest, error) {
	var f LoginRequest
	// DELETE RETURNINGで一度だけ使う。同じ認証応答を再送しても再利用できない。
	err := s.DB.QueryRowContext(ctx, `DELETE FROM login_requests WHERE state_hash=$1 AND browser_hash=$2 AND expires_at>now()
 RETURNING nonce, verifier`, hash(state), hash(browser)).Scan(&f.Nonce, &f.Verifier)
	return f, err
}

func (s PostgresStore) CreateSession(ctx context.Context, user Identity, token, oldToken string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	// メールアドレスではなく、検証済みのissuerとsubjectの組で同じ人か判断する。
	err = tx.QueryRowContext(ctx, `INSERT INTO users (issuer,subject,display_name) VALUES ($1,$2,$3)
 ON CONFLICT (issuer,subject) DO UPDATE SET display_name=EXCLUDED.display_name RETURNING id`, user.Issuer, user.Subject, user.Name).Scan(&id)
	if err != nil {
		return err
	}
	if oldToken != "" {
		if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash=$1", hash(oldToken)); err != nil {
			return err
		}
	}
	// Cookieの生の値は保存せず、SHA-256ハッシュだけを保存する。期限は固定8時間。
	_, err = tx.ExecContext(ctx, `INSERT INTO sessions (token_hash,user_id,expires_at) VALUES ($1,$2,now()+interval '8 hours')`, hash(token), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s PostgresStore) FindSession(ctx context.Context, token string) (Session, error) {
	var session Session
	err := s.DB.QueryRowContext(ctx, `SELECT u.id,u.display_name,s.expires_at FROM sessions s JOIN users u ON u.id=s.user_id
 WHERE s.token_hash=$1 AND s.expires_at>now()`, hash(token)).Scan(&session.UserID, &session.Name, &session.ExpiresAt)
	return session, err
}

func (s PostgresStore) DeleteSession(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash=$1", hash(token))
	return err
}
