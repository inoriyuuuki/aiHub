// Package core implements authentication, API tokens, projects and the
// module registry endpoint.
package core

import (
	"context"
	"embed"
	"fmt"
	"net/http"

	"github.com/aihub/aihub/internal/mcpx"
	"github.com/aihub/aihub/internal/modules"
	"github.com/aihub/aihub/internal/platform/db"
	"github.com/aihub/aihub/internal/platform/httpx"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Module is the core module.
type Module struct{}

// ID implements modules.Module.
func (Module) ID() string { return "core" }

// Version implements modules.Module.
func (Module) Version() string { return "1.0.0" }

// DependsOn implements modules.Module.
func (Module) DependsOn() []string { return nil }

// Migrations implements modules.Module.
func (Module) Migrations() []db.Migration {
	return []db.Migration{
		{ID: "20260809001_core_init", FS: &migrationsFS, File: "migrations/20260809001_core_init.sql"},
	}
}

// Register implements modules.Module.
func (Module) Register(r *httpx.Router, deps *modules.Deps) error {
	if deps.Cfg.AdminPasswordFile == "" {
		return fmt.Errorf("ADMIN_PASSWORD_FILE is required")
	}
	svc := NewService(deps)
	deps.Auth = svc // expose auth gateway to other modules
	deps.Extra["core.service"] = svc
	for _, t := range svc.mcpTools() {
		if err := deps.MCP.Add(t); err != nil {
			return err
		}
	}

	r.Group("/api/v1", func(r *httpx.Router) {
		r.Handle("POST", "/auth/login", svc.HandleLogin)
		r.Handle("POST", "/auth/logout", svc.RequireAuth(svc.HandleLogout))
		r.Handle("GET", "/auth/me", svc.RequireAuth(svc.HandleMe))
		r.Handle("POST", "/auth/password", svc.RequireWrite("auth")(svc.HandleChangePassword))
		r.Handle("GET", "/tokens", svc.RequireAuth(svc.HandleListTokens))
		r.Handle("POST", "/tokens", svc.RequireWrite("auth")(svc.HandleCreateToken))
		r.Handle("DELETE", "/tokens/{id}", svc.RequireDelete("auth")(svc.HandleRevokeToken))
		r.Handle("GET", "/projects", svc.RequireAuth(svc.HandleListProjects))
		r.Handle("POST", "/projects", svc.RequireWrite("projects")(svc.HandleCreateProject))
		r.Handle("GET", "/projects/{id}", svc.RequireAuth(svc.HandleGetProject))
		r.Handle("PATCH", "/projects/{id}", svc.RequireWrite("projects")(svc.HandleUpdateProject))
		r.Handle("DELETE", "/projects/{id}", svc.RequireDelete("projects")(svc.HandleArchiveProject))
		r.Handle("GET", "/modules", svc.RequireAuth(handleModules(deps)))
		r.Handle("GET", "/health", svc.HandleHealth)
	})
	return nil
}

// MCPTools implements modules.Module.
func (Module) MCPTools() []mcpx.ToolDef {
	return []mcpx.ToolDef{
		{
			Name:        "projects.read",
			Description: "列出或搜索项目（支持分页与归档过滤）",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword":  map[string]any{"type": "string"},
					"archived": map[string]any{"type": "boolean"},
					"page":     map[string]any{"type": "integer"},
					"pageSize": map[string]any{"type": "integer"},
				},
			},
			Group: "projects",
		},
	}
}

// Health implements modules.Module.
func (Module) Health(ctx context.Context, deps *modules.Deps) error {
	return deps.DB.Ping(ctx)
}

// handleModules serves the module registry endpoint: enabled modules with
// their versions, dependencies and frontend capabilities.
func handleModules(deps *modules.Deps) httpx.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type modInfo struct {
			ID        string   `json:"id"`
			Version   string   `json:"version"`
			DependsOn []string `json:"dependsOn"`
			Enabled   bool     `json:"enabled"`
			Tools     []string `json:"tools"`
		}
		out := []modInfo{}
		for _, m := range deps.Registry.All() {
			enabled := deps.Cfg.ModuleEnabled(m.ID())
			tools := []string{}
			if enabled {
				for _, t := range m.MCPTools() {
					tools = append(tools, t.Name)
				}
			}
			out = append(out, modInfo{ID: m.ID(), Version: m.Version(), DependsOn: m.DependsOn(), Enabled: enabled, Tools: tools})
		}
		httpx.JSON(w, 200, out)
	}
}
