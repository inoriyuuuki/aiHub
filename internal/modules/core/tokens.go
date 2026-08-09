package core

import (
	"net/http"
	"strconv"
	"time"

	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/aihub/aihub/internal/platform/security"
	"github.com/jackc/pgx/v5"
)

// tokenDTO is the API token representation (without the raw secret).
type tokenDTO struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	LastUsed  *time.Time `json:"lastUsedAt,omitempty"`
	Revoked   bool       `json:"revoked"`
}

// createTokenRequest creates a named token with scopes and optional TTL.
type createTokenRequest struct {
	Name     string   `json:"name"`
	Scopes   []string `json:"scopes"`
	TTLHours *int     `json:"ttlHours,omitempty"`
}

// createTokenResponse returns the token value exactly once.
type createTokenResponse struct {
	ID     int64    `json:"id"`
	Name   string   `json:"name"`
	Token  string   `json:"token"`
	Scopes []string `json:"scopes"`
}

// knownScopes is the set of acceptable token scopes.
var knownScopes = map[string]bool{
	"read": true, "write": true, "delete": true,
	"projects.read": true, "projects.write": true, "projects.delete": true,
	"prompts.read": true, "prompts.write": true, "prompts.delete": true,
	"skills.read": true, "skills.write": true, "skills.delete": true,
	"experts.read": true, "experts.write": true, "experts.delete": true,
	"mcp_catalog.read": true, "mcp_catalog.write": true, "mcp_catalog.delete": true,
	"auth.read": true, "auth.write": true,
	"mcp": true, "mcp.read": true, "mcp.write": true, "mcp.delete": true,
}

// HandleListTokens lists the caller's API tokens.
func (s *Service) HandleListTokens(w http.ResponseWriter, r *http.Request) {
	uid := httpx.UserID(r.Context())
	rows, err := s.db.Query(r.Context(), `
		SELECT id, name, scopes, created_at, expires_at, last_used_at, revoked_at
		FROM api_tokens WHERE user_id=$1 ORDER BY created_at DESC`, uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer rows.Close()
	out := []tokenDTO{}
	for rows.Next() {
		var t tokenDTO
		var revoked *time.Time
		if err := rows.Scan(&t.ID, &t.Name, &t.Scopes, &t.CreatedAt, &t.ExpiresAt, &t.LastUsed, &revoked); err != nil {
			httpx.WriteError(w, err)
			return
		}
		t.Revoked = revoked != nil
		out = append(out, t)
	}
	httpx.JSON(w, http.StatusOK, out)
}

// HandleCreateToken creates an API token and returns the raw secret once.
func (s *Service) HandleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req createTokenRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if req.Name == "" {
		httpx.WriteError(w, httpx.ErrUnprocessable("Token 名称不能为空"))
		return
	}
	if len(req.Scopes) == 0 {
		req.Scopes = []string{"read"}
	}
	for _, sc := range req.Scopes {
		if !knownScopes[sc] {
			httpx.WriteError(w, httpx.ErrUnprocessable("未知 scope: "+sc))
			return
		}
	}
	var expires *time.Time
	if req.TTLHours != nil && *req.TTLHours > 0 {
		t := time.Now().Add(time.Duration(*req.TTLHours) * time.Hour)
		expires = &t
	} else if s.cfg.APITokenTTL > 0 {
		t := time.Now().Add(s.cfg.APITokenTTL)
		expires = &t
	}
	raw, err := security.RandomToken(32)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	uid := httpx.UserID(r.Context())
	var id int64
	err = s.db.QueryRow(r.Context(), `
		INSERT INTO api_tokens (name, user_id, token_hash, scopes, expires_at)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		req.Name, uid, security.HashToken(raw), req.Scopes, expires).Scan(&id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, createTokenResponse{ID: id, Name: req.Name, Token: raw, Scopes: req.Scopes})
}

// HandleRevokeToken revokes a token by id.
func (s *Service) HandleRevokeToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 token id"))
		return
	}
	uid := httpx.UserID(r.Context())
	tag, err := s.db.Exec(r.Context(),
		`UPDATE api_tokens SET revoked_at=now() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, id, uid)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		_ = s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM api_tokens WHERE id=$1 AND user_id=$2)`, id, uid).Scan(&exists)
		if !exists {
			httpx.WriteError(w, httpx.ErrNotFound("Token 不存在"))
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true}) // already revoked
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

var _ = pgx.ErrNoRows
