package core

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/aihub/aihub/internal/platform/security"
	"github.com/jackc/pgx/v5"
)

const (
	cookieSession = "aihub_session"
	cookieCSRF    = "aihub_csrf"
	headerCSRF    = "X-CSRF-Token"
)

// loginRequest is the login payload.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// loginResponse returns session and CSRF cookies plus user info.
type loginResponse struct {
	User userDTO `json:"user"`
}

type userDTO struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"isAdmin"`
}

// HandleLogin authenticates a user and issues a session cookie.
func (s *Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	key := r.RemoteAddr + "|" + req.Username
	if !s.rl.allow(key) {
		httpx.WriteError(w, httpx.ErrTooManyRequests("登录尝试过于频繁，请稍后再试"))
		return
	}
	ctx := r.Context()
	u, err := s.verifyCredentials(ctx, req.Username, req.Password)
	if err != nil {
		httpx.WriteError(w, httpx.ErrUnauthorized("用户名或密码错误"))
		return
	}
	s.rl.reset(key)
	s.audit(ctx, u.ID, "login", "auth", "", nil, r.RemoteAddr)

	raw, err := security.RandomToken(32)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	expires := time.Now().Add(s.cfg.SessionTTL)
	_, err = s.db.Exec(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1,$2,$3)`,
		security.HashToken(raw), u.ID, expires)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	csrf, err := security.RandomHex(24)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	secure := r.TLS != nil || strings.HasPrefix(s.cfg.PublicBaseURL, "https://")
	http.SetCookie(w, &http.Cookie{
		Name: cookieSession, Value: raw, Path: "/", Expires: expires,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name: cookieCSRF, Value: csrf, Path: "/", Expires: expires,
		HttpOnly: false, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
	httpx.JSON(w, http.StatusOK, loginResponse{User: userDTO{ID: u.ID, Username: u.Username, IsAdmin: u.IsAdmin}})
}

// HandleLogout revokes the current session.
func (s *Service) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieSession); err == nil && c.Value != "" {
		_, _ = s.db.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE token_hash=$1 AND revoked_at IS NULL`,
			security.HashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: cookieSession, Value: "", Path: "/", MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: cookieCSRF, Value: "", Path: "/", MaxAge: -1})
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// HandleMe returns the current user.
func (s *Service) HandleMe(w http.ResponseWriter, r *http.Request) {
	u, err := s.userByID(r.Context(), httpx.UserID(r.Context()))
	if err != nil {
		httpx.WriteError(w, httpx.ErrUnauthorized("会话已失效"))
		return
	}
	httpx.JSON(w, http.StatusOK, userDTO{ID: u.ID, Username: u.Username, IsAdmin: u.IsAdmin})
}

// changePasswordRequest is the password change payload.
type changePasswordRequest struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

// HandleChangePassword changes the current user's password and revokes all
// other sessions and tokens.
func (s *Service) HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req changePasswordRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if len(req.NewPassword) < 8 {
		httpx.WriteError(w, httpx.ErrUnprocessable("新密码至少 8 位"))
		return
	}
	u, err := s.userByID(r.Context(), httpx.UserID(r.Context()))
	if err != nil {
		httpx.WriteError(w, httpx.ErrUnauthorized("会话已失效"))
		return
	}
	ok, err := security.VerifyPassword(u.PasswordHash, req.OldPassword)
	if err != nil || !ok {
		httpx.WriteError(w, httpx.ErrUnprocessable("原密码错误"))
		return
	}
	hash, err := security.HashPassword(req.NewPassword)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	if _, err := tx.Exec(r.Context(), `UPDATE users SET password_hash=$1, updated_at=now() WHERE id=$2`, hash, u.ID); err != nil {
		httpx.WriteError(w, err)
		return
	}
	// Revoke all sessions and tokens except the current session.
	var curHash string
	if c, err := r.Cookie(cookieSession); err == nil {
		curHash = security.HashToken(c.Value)
	}
	if _, err := tx.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND (revoked_at IS NULL) AND token_hash <> $2`, u.ID, curHash); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE api_tokens SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, u.ID); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteError(w, err)
		return
	}
	s.audit(r.Context(), u.ID, "password_change", "auth", "", nil, r.RemoteAddr)
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// HandleHealth reports service health (unauthenticated).
func (s *Service) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.db.Ping(r.Context()); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type userRow struct {
	ID           int64
	Username     string
	PasswordHash string
	IsAdmin      bool
}

// dummyHash is used to equalize login timing for unknown usernames.
var dummyHash, _ = security.HashPassword("dummy-password-for-timing")

func (s *Service) verifyCredentials(ctx context.Context, username, password string) (userRow, error) {
	var u userRow
	err := s.db.QueryRow(ctx,
		`SELECT id, username, password_hash, is_admin FROM users WHERE username=$1`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin)
	if err != nil {
		// Run a dummy argon2 verify so unknown users take the same time.
		_, _ = security.VerifyPassword(dummyHash, password)
		return u, errors.New("invalid credentials")
	}
	ok, err := security.VerifyPassword(u.PasswordHash, password)
	if err != nil || !ok {
		return u, errors.New("invalid credentials")
	}
	return u, nil
}

func (s *Service) userByID(ctx context.Context, id int64) (userRow, error) {
	var u userRow
	err := s.db.QueryRow(ctx,
		`SELECT id, username, password_hash, is_admin FROM users WHERE id=$1`, id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin)
	if err == pgx.ErrNoRows {
		return u, errors.New("user not found")
	}
	return u, err
}

func (s *Service) authenticateSession(ctx context.Context, raw string) (context.Context, error) {
	var uid int64
	err := s.db.QueryRow(ctx,
		`SELECT user_id FROM sessions WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at > now()`,
		security.HashToken(raw)).Scan(&uid)
	if err != nil {
		return ctx, nil // expired/invalid sessions are treated as anonymous
	}
	return httpx.WithUserID(ctx, uid), nil
}

func (s *Service) authenticateToken(ctx context.Context, raw string) (context.Context, error) {
	var uid int64
	var scopes []string
	err := s.db.QueryRow(ctx,
		`SELECT user_id, scopes FROM api_tokens WHERE token_hash=$1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())`,
		security.HashToken(raw)).Scan(&uid, &scopes)
	if err != nil {
		return nil, httpx.ErrUnauthorized("API Token 无效或已撤销")
	}
	_, _ = s.db.Exec(ctx, `UPDATE api_tokens SET last_used_at=now() WHERE token_hash=$1`, security.HashToken(raw))
	ctx = httpx.WithUserID(ctx, uid)
	ctx = httpx.WithAuthMethod(ctx, "token")
	ctx = httpx.WithTokenScopes(ctx, scopes)
	return ctx, nil
}
