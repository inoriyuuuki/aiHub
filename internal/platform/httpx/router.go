package httpx

import (
	"net/http"
	"strings"
)

// HandlerFunc is an alias for http.HandlerFunc.
type HandlerFunc = http.HandlerFunc

// Router is a thin wrapper over http.ServeMux with grouped prefixes.
type Router struct {
	mux  *http.ServeMux
	base string
}

// NewRouter creates a root router.
func NewRouter() *Router { return &Router{mux: http.NewServeMux(), base: ""} }

// Mux exposes the underlying mux (used for static file mounting).
func (r *Router) Mux() *http.ServeMux { return r.mux }

// Group mounts a sub-router at prefix (e.g. "/api/v1").
func (r *Router) Group(prefix string, fn func(*Router)) {
	sub := &Router{mux: r.mux, base: r.base + prefix}
	fn(sub)
}

// Handle registers a pattern "METHOD /path" with optional {param} wildcards.
func (r *Router) Handle(method, pattern string, h http.HandlerFunc) {
	full := r.base + pattern
	if method != "" {
		full = method + " " + full
	}
	r.mux.Handle(full, h)
}

// HandleFunc is an alias of Handle for any method.
func (r *Router) HandleFunc(pattern string, h http.HandlerFunc) { r.Handle("", pattern, h) }

// Static serves files from an embedded FS at a prefix.
func (r *Router) Static(prefix string, fs http.FileSystem) {
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	r.mux.Handle(r.base+prefix, http.StripPrefix(r.base+prefix, http.FileServer(fs)))
}
