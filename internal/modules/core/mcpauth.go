package core

import (
	"net/http"
	"strings"

	"github.com/aihub/aihub/internal/platform/httpx"
)

// AuthenticateMCP implements mcpx.Authenticator using API token scopes.
// A valid token needs at least one read-capable scope to access the MCP.
func (s *Service) AuthenticateMCP(r *http.Request) ([]string, error) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return nil, httpx.ErrUnauthorized("需要 Bearer API Token")
	}
	ctx, err := s.authenticateToken(r.Context(), strings.TrimPrefix(h, "Bearer "))
	if err != nil {
		return nil, err
	}
	if httpx.AuthMethod(ctx) != "token" {
		return nil, httpx.ErrUnauthorized("需要 Bearer API Token")
	}
	scopes := httpx.TokenScopes(ctx)
	ok := false
	for _, sc := range scopes {
		if sc == "read" || sc == "mcp" || sc == "mcp.read" || strings.HasSuffix(sc, ".read") {
			ok = true
			break
		}
	}
	if !ok {
		return nil, httpx.ErrForbidden("Token 需要 read 或 mcp 作用域")
	}
	return scopes, nil
}
