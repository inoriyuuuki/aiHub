// Package server implements the AIHub HTTP server: REST API, static frontend
// and a Streamable HTTP MCP endpoint.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aihub/aihub/internal/web"
)

// Config configures the AIHub server; values come from the environment.
type Config struct {
	Addr      string
	DataDir   string
	AdminUser string
	AdminPass string
}

// FromEnv builds a Config from environment variables.
//
//	AIHUB_PORT          - listen port (default 8080, falls back to $PORT)
//	AIHUB_DATA_DIR      - data directory (default ./data)
//	AIHUB_ADMIN_USER    - admin username (default admin)
//	AIHUB_ADMIN_PASSWORD- admin password (default: admin_password.txt, then "admin")
func FromEnv() *Config {
	cfg := &Config{
		Addr:      ":" + getenv("AIHUB_PORT", getenv("PORT", "8080")),
		DataDir:   getenv("AIHUB_DATA_DIR", "data"),
		AdminUser: getenv("AIHUB_ADMIN_USER", "admin"),
		AdminPass: os.Getenv("AIHUB_ADMIN_PASSWORD"),
	}
	if cfg.AdminPass == "" {
		if b, err := os.ReadFile("admin_password.txt"); err == nil {
			cfg.AdminPass = strings.TrimSpace(string(b))
		}
	}
	if cfg.AdminPass == "" {
		cfg.AdminPass = "admin"
	}
	return cfg
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Server is the AIHub HTTP server.
type Server struct {
	cfg      *Config
	logger   *slog.Logger
	tokens   *TokenStore
	registry *Registry
	started  time.Time
}

// New creates a server with the given config.
func New(cfg *Config, logger *slog.Logger) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}
	tokens, err := LoadTokenStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	registry, err := LoadRegistry(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg:      cfg,
		logger:   logger,
		tokens:   tokens,
		registry: registry,
		started:  time.Now(),
	}, nil
}

// Run starts the server and blocks until ctx is cancelled or a fatal error.
func Run(ctx context.Context, logger *slog.Logger) error {
	srv, err := New(FromEnv(), logger)
	if err != nil {
		return err
	}
	httpSrv := &http.Server{
		Addr:              srv.cfg.Addr,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		srv.logger.Info("aihub-server listening",
			"addr", srv.cfg.Addr, "data_dir", srv.cfg.DataDir)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// routes builds the HTTP handler tree.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("/api/v1/tokens", s.requireAuth(s.handleTokens))
	mux.HandleFunc("/api/v1/tokens/", s.requireAuth(s.handleTokenByID))
	mux.HandleFunc("/api/v1/skills/publish", s.requireAuth(s.handlePublishSkill))
	mux.HandleFunc("/api/v1/skills/install-manifest", s.requireAuth(s.handleSkillManifest))
	mux.HandleFunc("/api/v1/expert-packs/install-manifest", s.requireAuth(s.handleExpertManifest))
	mux.HandleFunc("/api/v1/mcp/install-manifest", s.requireAuth(s.handleMCPManifest))
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/mcp", s.handleMCPHTTP)
	mux.HandleFunc("/", s.handleFrontend)
	return logRequests(s.logger, mux)
}

func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		logger.Debug("http", "method", r.Method, "path", r.URL.Path, "status", sw.status)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// requireAuth rejects requests without a valid bearer token.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimPrefix(h, "Bearer ")
		}
		if token == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "缺少 Authorization: Bearer <token>"})
			return
		}
		if s.tokens.Verify(token) == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "无效或过期的 token"})
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// handleHealth reports liveness.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": "1.0.0",
		"uptime":  time.Since(s.started).String(),
	})
}

// DistFS is exposed for tests and tooling.
var DistFS = web.DistFS
