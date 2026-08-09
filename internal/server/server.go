// Package server wires and runs the AIHub HTTP server.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/aihub/aihub/internal/config"
	"github.com/aihub/aihub/internal/mcpx"
	"github.com/aihub/aihub/internal/modules"
	"github.com/aihub/aihub/internal/modules/core"
	"github.com/aihub/aihub/internal/modules/experts"
	"github.com/aihub/aihub/internal/modules/mcpcat"
	"github.com/aihub/aihub/internal/modules/prompts"
	"github.com/aihub/aihub/internal/modules/skills"
	"github.com/aihub/aihub/internal/platform/db"
	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/aihub/aihub/internal/platform/storage"
	"github.com/aihub/aihub/internal/web"
)

// App holds the running server dependencies.
type App struct {
	Cfg   *config.Config
	DB    *db.Pool
	Store *storage.Storage
	Reg   *modules.Registry
	MCP   *mcpx.Registry
	Deps  *modules.Deps
	Mux   *http.ServeMux
}

// New builds all server components (config, db, storage, modules, routes).
func New(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*App, error) {
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	store, err := storage.New(ctx, cfg.MinIOEndpoint, cfg.MinIOAccessKey, cfg.MinIOSecretKey, cfg.MinIOUseSSL, cfg.MinIOBucket)
	if err != nil {
		pool.Close()
		return nil, err
	}
	reg := modules.NewRegistry()
	mcpReg := mcpx.NewRegistry()
	deps := &modules.Deps{
		Cfg: cfg, DB: pool, Store: store, Logger: logger,
		Registry: reg, MCP: mcpReg, Extra: map[string]any{},
	}
	mods := []modules.Module{core.Module{}, prompts.Module{}, skills.Module{}, experts.Module{}, mcpcat.Module{}}
	for _, m := range mods {
		if err := reg.Register(m); err != nil {
			pool.Close()
			return nil, err
		}
	}
	if err := db.Migrate(ctx, pool, reg.Migrations()); err != nil {
		pool.Close()
		return nil, err
	}
	// Validate that enabled modules form a closed dependency set (core required).
	enabledSet := map[string]bool{}
	for _, m := range reg.Enabled(cfg) {
		enabledSet[m.ID()] = true
	}
	for _, m := range reg.All() {
		if !enabledSet[m.ID()] {
			continue
		}
		for _, dep := range m.DependsOn() {
			if !enabledSet[dep] {
				pool.Close()
				return nil, fmt.Errorf("模块 %q 依赖的 %q 未启用", m.ID(), dep)
			}
		}
	}
	router := httpx.NewRouter()
	for _, m := range reg.Enabled(cfg) {
		if err := m.Register(router, deps); err != nil {
			pool.Close()
			return nil, err
		}
	}
	router.Group("/api/v1", func(r *httpx.Router) {
		r.Handle("GET", "/mcp/tools", deps.Auth.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
			type toolInfo struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				InputSchema map[string]any `json:"inputSchema"`
				Write       bool           `json:"write"`
				Delete      bool           `json:"delete"`
				Group       string         `json:"group"`
			}
			out := []toolInfo{}
			for _, t := range mcpReg.All() {
				out = append(out, toolInfo{t.Name, t.Description, t.InputSchema, t.Write, t.Delete, t.Group})
			}
			httpx.JSON(w, http.StatusOK, out)
		}))
	})
	coreSvc, _ := deps.Extra["core.service"].(*core.Service)
	if coreSvc == nil {
		pool.Close()
		return nil, errors.New("core service not initialized")
	}
	if err := coreSvc.BootstrapAdmin(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if err := seedTemplates(ctx, pool); err != nil {
		logger.Warn("seed templates failed", "error", err)
	}
	webHandler, err := web.Handler()
	if err != nil {
		pool.Close()
		return nil, err
	}
	router.HandleFunc("/", webHandler.ServeHTTP)
	mcpHandler := mcpx.NewStreamableHTTPHandler(mcpReg, coreSvc, logger)
	router.HandleFunc("/mcp", mcpHandler.ServeHTTP)
	return &App{
		Cfg: cfg, DB: pool, Store: store, Reg: reg, MCP: mcpReg, Deps: deps, Mux: router.Mux(),
	}, nil
}

// Handler returns the full HTTP handler with middleware.
func (a *App) Handler(logger *slog.Logger) http.Handler {
	coreSvc, _ := a.Deps.Extra["core.service"].(*core.Service)
	var csrf http.Handler = a.Mux
	if coreSvc != nil {
		csrf = core.CSRFMiddleware(a.Mux)
	}
	return httpx.RequestIDMiddleware(
		httpx.Recovery(logger,
			httpx.LoggingMiddleware(logger, csrf)))
}

// Close releases resources.
func (a *App) Close() {
	a.DB.Close()
}

// Run starts an HTTP server until ctx is cancelled.
func Run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	app, err := New(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer app.Close()
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           app.Handler(logger),
		ReadHeaderTimeout: 10 * time.Second,
	}
	// Background cleanup of expired sessions and revoked tokens.
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := app.DB.Exec(context.Background(),
					`DELETE FROM sessions WHERE expires_at < now() - interval '1 day' OR revoked_at IS NOT NULL`); err != nil {
					logger.Warn("session cleanup failed", "error", err)
				}
			}
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("aihub-server listening", "addr", cfg.HTTPAddr, "base_url", cfg.PublicBaseURL)
		errCh <- srv.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}
