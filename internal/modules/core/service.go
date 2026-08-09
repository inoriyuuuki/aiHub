package core

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/aihub/aihub/internal/config"
	"github.com/aihub/aihub/internal/modules"
	"github.com/aihub/aihub/internal/platform/db"
	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/aihub/aihub/internal/platform/security"
	"github.com/aihub/aihub/internal/platform/storage"
)

var errEmptyAdminPassword = errors.New("ADMIN_PASSWORD_FILE is empty")

// Service implements auth, tokens, projects and the AuthGateway interface.
type Service struct {
	deps  *modules.Deps
	db    *db.Pool
	cfg   *config.Config
	store *storage.Storage
	log   *slog.Logger
	rl    *rateLimiter
}

// NewService builds the core service and bootstraps the admin account.
func NewService(deps *modules.Deps) *Service {
	return &Service{
		deps:  deps,
		db:    deps.DB,
		cfg:   deps.Cfg,
		store: deps.Store,
		log:   deps.Logger,
		rl:    newRateLimiter(deps.Cfg.LoginMaxAttempts, deps.Cfg.LoginWindow),
	}
}

// audit records a lightweight audit-log entry (best effort).
func (s *Service) audit(ctx context.Context, userID int64, action, resourceType, resourceID string, detail any, ip string) {
	data, _ := json.Marshal(detail)
	_, _ = s.db.Exec(ctx,
		`INSERT INTO audit_log (user_id, action, resource_type, resource_id, detail, ip) VALUES ($1,$2,$3,$4,$5,$6)`,
		nullableID(userID), action, resourceType, resourceID, data, ip)
}

func nullableID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// BootstrapAdmin creates the initial admin account if none exists.
func (s *Service) BootstrapAdmin(ctx context.Context) error {
	var count int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	pw, err := os.ReadFile(s.cfg.AdminPasswordFile)
	if err != nil {
		return err
	}
	password := strings.TrimSpace(string(pw))
	if password == "" {
		return errEmptyAdminPassword
	}
	hash, err := security.HashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx,
		`INSERT INTO users (username, password_hash, is_admin) VALUES ($1,$2,true)`,
		s.cfg.AdminUsername, hash)
	return err
}

// Authenticate resolves either a bearer token or a session cookie and returns
// an enriched context. Anonymous requests get a plain context.
func (s *Service) Authenticate(r *http.Request) (context.Context, error) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		raw := strings.TrimPrefix(h, "Bearer ")
		return s.authenticateToken(r.Context(), raw)
	}
	if c, err := r.Cookie(cookieSession); err == nil && c.Value != "" {
		return s.authenticateSession(r.Context(), c.Value)
	}
	return r.Context(), nil
}

// RequireAuth implements modules.AuthGateway.
func (s *Service) RequireAuth(h httpx.HandlerFunc) httpx.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, err := s.Authenticate(r)
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		if httpx.UserID(ctx) == 0 {
			httpx.WriteError(w, httpx.ErrUnauthorized("请先登录"))
			return
		}
		h(w, r.WithContext(ctx))
	}
}

// RequireToken implements modules.AuthGateway.
func (s *Service) RequireToken(scopes ...string) func(httpx.HandlerFunc) httpx.HandlerFunc {
	return func(h httpx.HandlerFunc) httpx.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ctx, err := s.Authenticate(r)
			if err != nil {
				httpx.WriteError(w, err)
				return
			}
			if httpx.AuthMethod(ctx) != "token" {
				httpx.WriteError(w, httpx.ErrUnauthorized("需要 Bearer API Token"))
				return
			}
			got := httpx.TokenScopes(ctx)
			if !hasAnyScope(got, scopes) {
				httpx.WriteError(w, httpx.ErrForbidden("Token 缺少所需权限："+strings.Join(scopes, ",")))
				return
			}
			h(w, r.WithContext(ctx))
		}
	}
}

// RequireWrite implements modules.AuthGateway.
func (s *Service) RequireWrite(group string) func(httpx.HandlerFunc) httpx.HandlerFunc {
	return func(h httpx.HandlerFunc) httpx.HandlerFunc {
		return s.requireScopes(h, group, "write")
	}
}

// RequireDelete implements modules.AuthGateway.
func (s *Service) RequireDelete(group string) func(httpx.HandlerFunc) httpx.HandlerFunc {
	return func(h httpx.HandlerFunc) httpx.HandlerFunc {
		return s.requireScopes(h, group, "delete")
	}
}

// requireScopes allows sessions; API-token callers must carry the generic
// scope or the group-specific scope for the action.
func (s *Service) requireScopes(h httpx.HandlerFunc, group, action string) httpx.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, err := s.Authenticate(r)
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		if httpx.UserID(ctx) == 0 {
			httpx.WriteError(w, httpx.ErrUnauthorized("请先登录"))
			return
		}
		if httpx.AuthMethod(ctx) == "token" {
			scopes := httpx.TokenScopes(ctx)
			if !hasAnyScope(scopes, []string{action, group + "." + action}) {
				httpx.WriteError(w, httpx.ErrForbidden("Token 缺少 "+group+"."+action+" 权限"))
				return
			}
		}
		h(w, r.WithContext(ctx))
	}
}

// TokenScopes implements modules.AuthGateway.
func (s *Service) TokenScopes(ctx context.Context) []string { return httpx.TokenScopes(ctx) }

func hasAnyScope(got, want []string) bool {
	if len(want) == 0 {
		return true
	}
	set := map[string]bool{}
	for _, g := range got {
		set[g] = true
	}
	for _, w := range want {
		if set[w] {
			return true
		}
	}
	return false
}
