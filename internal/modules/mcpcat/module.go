// Package mcpcat implements the third-party MCP catalog: definitions,
// versions, profiles and Codex install manifests.
package mcpcat

import (
	"context"
	"embed"

	"github.com/aihub/aihub/internal/config"
	"github.com/aihub/aihub/internal/mcpx"
	"github.com/aihub/aihub/internal/modules"
	"github.com/aihub/aihub/internal/platform/db"
	"github.com/aihub/aihub/internal/platform/httpx"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Module implements the MCP catalog module.
type Module struct{}

// ID implements modules.Module.
func (Module) ID() string { return "mcp_catalog" }

// Version implements modules.Module.
func (Module) Version() string { return "1.0.0" }

// DependsOn implements modules.Module.
func (Module) DependsOn() []string { return []string{"core"} }

// Migrations implements modules.Module.
func (Module) Migrations() []db.Migration {
	return []db.Migration{
		{ID: "20260809005_mcp", FS: &migrationsFS, File: "migrations/20260809005_mcp.sql"},
	}
}

// Service holds MCP catalog dependencies.
type Service struct {
	db  *db.Pool
	cfg *config.Config
}

// Register implements modules.Module.
func (Module) Register(r *httpx.Router, deps *modules.Deps) error {
	svc := &Service{db: deps.DB, cfg: deps.Cfg}
	for _, t := range svc.mcpTools() {
		if err := deps.MCP.Add(t); err != nil {
			return err
		}
	}
	auth := deps.Auth
	r.Group("/api/v1", func(r *httpx.Router) {
		r.Handle("GET", "/mcp/definitions", auth.RequireAuth(svc.handleListDefinitions))
		r.Handle("POST", "/mcp/definitions", auth.RequireWrite("mcp_catalog")(svc.handleCreateDefinition))
		r.Handle("GET", "/mcp/definitions/{id}", auth.RequireAuth(svc.handleGetDefinition))
		r.Handle("PATCH", "/mcp/definitions/{id}", auth.RequireWrite("mcp_catalog")(svc.handleUpdateDefinition))
		r.Handle("GET", "/mcp/definitions/{id}/versions", auth.RequireAuth(svc.handleListDefVersions))
		r.Handle("POST", "/mcp/definitions/{id}/versions", auth.RequireWrite("mcp_catalog")(svc.handleAddDefVersion))
		r.Handle("DELETE", "/mcp/definitions/{id}", auth.RequireDelete("mcp_catalog")(svc.handleArchiveDefinition))

		r.Handle("GET", "/mcp/profiles", auth.RequireAuth(svc.handleListProfiles))
		r.Handle("POST", "/mcp/profiles", auth.RequireWrite("mcp_catalog")(svc.handleCreateProfile))
		r.Handle("GET", "/mcp/profiles/{id}", auth.RequireAuth(svc.handleGetProfile))
		r.Handle("PATCH", "/mcp/profiles/{id}", auth.RequireWrite("mcp_catalog")(svc.handleUpdateProfile))
		r.Handle("GET", "/mcp/profiles/{id}/items", auth.RequireAuth(svc.handleListProfileItems))
		r.Handle("POST", "/mcp/profiles/{id}/items", auth.RequireWrite("mcp_catalog")(svc.handleAddProfileItem))
		r.Handle("DELETE", "/mcp/profiles/{id}/items/{itemId}", auth.RequireDelete("mcp_catalog")(svc.handleRemoveProfileItem))
		r.Handle("DELETE", "/mcp/profiles/{id}", auth.RequireDelete("mcp_catalog")(svc.handleArchiveProfile))

		r.Handle("GET", "/mcp/install-manifest", auth.RequireAuth(svc.handleInstallManifest))
	})
	return nil
}

// MCPTools implements modules.Module.
func (Module) MCPTools() []mcpx.ToolDef {
	return []mcpx.ToolDef{
		{
			Name:        "mcp_catalog.read",
			Description: "搜索 MCP 定义/Profile 或获取 Codex 安装清单",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword":  map[string]any{"type": "string"},
					"profile":  map[string]any{"type": "string"},
					"manifest": map[string]any{"type": "boolean"},
					"page":     map[string]any{"type": "integer"},
					"pageSize": map[string]any{"type": "integer"},
				},
			},
			Group: "mcp_catalog",
		},
		{
			Name:        "mcp_catalog.write",
			Description: "创建 MCP Profile 或定义",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action":    map[string]any{"type": "string", "enum": []any{"create_definition", "create_profile"}},
					"name":      map[string]any{"type": "string"},
					"slug":      map[string]any{"type": "string"},
					"transport": map[string]any{"type": "string"},
				},
				"required": []any{"action"},
			},
			Write: true,
			Group: "mcp_catalog",
		},
		{
			Name:        "mcp_catalog.delete",
			Description: "归档 MCP 定义或 Profile",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind": map[string]any{"type": "string", "enum": []any{"definition", "profile"}},
					"slug": map[string]any{"type": "string"},
				},
				"required": []any{"kind", "slug"},
			},
			Write:  true,
			Delete: true,
			Group:  "mcp_catalog",
		},
	}
}

// Health implements modules.Module.
func (Module) Health(ctx context.Context, deps *modules.Deps) error { return deps.DB.Ping(ctx) }
