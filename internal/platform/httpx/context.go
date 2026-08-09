package httpx

import (
	"context"
)

type ctxKey int

const (
	ctxUserID ctxKey = iota
	ctxAuthMethod
	ctxTokenScopes
	ctxRequestID
	ctxCSRFToken
)

// UserID extracts the authenticated user id (0 when anonymous).
func UserID(ctx context.Context) int64 {
	id, _ := ctx.Value(ctxUserID).(int64)
	return id
}

// WithUserID stores the user id.
func WithUserID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, ctxUserID, id)
}

// AuthMethod returns "session", "token" or "".
func AuthMethod(ctx context.Context) string {
	m, _ := ctx.Value(ctxAuthMethod).(string)
	return m
}

// WithAuthMethod stores the auth method.
func WithAuthMethod(ctx context.Context, m string) context.Context {
	return context.WithValue(ctx, ctxAuthMethod, m)
}

// TokenScopes returns the API token scopes from context (nil when not token auth).
func TokenScopes(ctx context.Context) []string {
	s, _ := ctx.Value(ctxTokenScopes).([]string)
	return s
}

// WithTokenScopes stores token scopes.
func WithTokenScopes(ctx context.Context, s []string) context.Context {
	return context.WithValue(ctx, ctxTokenScopes, s)
}

// RequestID returns the request id header.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(ctxRequestID).(string)
	return id
}

// WithRequestID stores a request id.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxRequestID, id)
}

// CSRFToken returns the CSRF token bound to this request.
func CSRFToken(ctx context.Context) string {
	t, _ := ctx.Value(ctxCSRFToken).(string)
	return t
}

// WithCSRFToken stores the CSRF token.
func WithCSRFToken(ctx context.Context, t string) context.Context {
	return context.WithValue(ctx, ctxCSRFToken, t)
}
