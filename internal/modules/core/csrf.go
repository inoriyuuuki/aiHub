package core

import (
	"net/http"
	"strings"

	"github.com/aihub/aihub/internal/platform/httpx"
)

// CSRFMiddleware enforces the double-submit cookie check for
// cookie-authenticated state-changing requests. Requests authenticated with a
// Bearer token (API tokens) are exempt because tokens are not subject to CSRF.
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			next.ServeHTTP(w, r)
			return
		}
		// Only cookie-authenticated requests need the header.
		sess, err := r.Cookie(cookieSession)
		if err != nil || sess.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			next.ServeHTTP(w, r)
			return
		}
		csrfCookie, err := r.Cookie(cookieCSRF)
		if err != nil || csrfCookie.Value == "" {
			httpx.WriteError(w, httpx.ErrForbidden("缺少 CSRF Cookie"))
			return
		}
		if r.Header.Get(headerCSRF) != csrfCookie.Value {
			httpx.WriteError(w, httpx.ErrForbidden("CSRF 校验失败"))
			return
		}
		next.ServeHTTP(w, r.WithContext(httpx.WithCSRFToken(r.Context(), csrfCookie.Value)))
	})
}
