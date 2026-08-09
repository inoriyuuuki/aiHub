package server

import (
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

// handleFrontend serves the embedded frontend with SPA fallback to index.html.
func (s *Server) handleFrontend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	dist, err := fs.Sub(DistFS, "dist")
	if err != nil {
		http.Error(w, "frontend assets unavailable", http.StatusInternalServerError)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	data, err := fs.ReadFile(dist, path)
	if err != nil {
		// SPA fallback: let the client-side router handle it.
		data, err = fs.ReadFile(dist, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		path = "index.html"
	}
	ct := mime.TypeByExtension(filepath.Ext(path))
	if ct == "" {
		ct = "text/html; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}
