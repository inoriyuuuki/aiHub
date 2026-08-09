// Package web embeds the built frontend static assets.
package web

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the embedded static filesystem.
func FS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}

// Handler serves static assets with SPA fallback to index.html.
func Handler() (http.Handler, error) {
	sub, err := FS()
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// API and MCP paths must never fall back to the SPA shell.
		if strings.HasPrefix(path, "/api/") || path == "/api" || strings.HasPrefix(path, "/mcp") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": "not_found", "message": "接口不存在"},
			})
			return
		}
		if path == "/" {
			path = "/index.html"
		}
		if _, err := fs.Stat(sub, path[1:]); err != nil {
			// SPA fallback
			http.ServeFileFS(w, r, sub, "index.html")
			return
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}
