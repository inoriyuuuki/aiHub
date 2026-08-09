// Package experts implements expert packs: locked skill collections with a
// generated coordinator skill and a deterministic build.
package experts

import (
	"context"
	"embed"

	"github.com/aihub/aihub/internal/config"
	"github.com/aihub/aihub/internal/mcpx"
	"github.com/aihub/aihub/internal/modules"
	"github.com/aihub/aihub/internal/platform/db"
	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/aihub/aihub/internal/platform/storage"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Module implements the experts module.
type Module struct{}

// ID implements modules.Module.
func (Module) ID() string { return "experts" }

// Version implements modules.Module.
func (Module) Version() string { return "1.0.0" }

// DependsOn implements modules.Module.
func (Module) DependsOn() []string { return []string{"core", "skills"} }

// Migrations implements modules.Module.
func (Module) Migrations() []db.Migration {
	return []db.Migration{
		{ID: "20260809004_experts", FS: &migrationsFS, File: "migrations/20260809004_experts.sql"},
	}
}

// Service holds experts module dependencies.
type Service struct {
	db    *db.Pool
	store *storage.Storage
	cfg   *config.Config
}

// Register implements modules.Module.
func (Module) Register(r *httpx.Router, deps *modules.Deps) error {
	svc := &Service{db: deps.DB, store: deps.Store, cfg: deps.Cfg}
	for _, t := range svc.mcpTools() {
		if err := deps.MCP.Add(t); err != nil {
			return err
		}
	}
	auth := deps.Auth
	r.Group("/api/v1", func(r *httpx.Router) {
		r.Handle("GET", "/expert-packs", auth.RequireAuth(svc.handleListPacks))
		r.Handle("POST", "/expert-packs", auth.RequireWrite("experts")(svc.handleCreatePack))
		r.Handle("GET", "/expert-packs/{id}", auth.RequireAuth(svc.handleGetPack))
		r.Handle("PATCH", "/expert-packs/{id}", auth.RequireWrite("experts")(svc.handleUpdatePack))
		r.Handle("GET", "/expert-packs/{id}/members", auth.RequireAuth(svc.handleListMembers))
		r.Handle("POST", "/expert-packs/{id}/members", auth.RequireWrite("experts")(svc.handleAddMember))
		r.Handle("DELETE", "/expert-packs/{id}/members/{skillId}", auth.RequireDelete("experts")(svc.handleRemoveMember))
		r.Handle("POST", "/expert-packs/{id}/build", auth.RequireWrite("experts")(svc.handleBuild))
		r.Handle("GET", "/expert-packs/{id}/versions", auth.RequireAuth(svc.handleListVersions))
		r.Handle("GET", "/expert-packs/{id}/versions/{v}/download", auth.RequireAuth(svc.handleDownload))
		r.Handle("GET", "/expert-packs/install-manifest", auth.RequireAuth(svc.handleInstallManifest))
		r.Handle("DELETE", "/expert-packs/{id}", auth.RequireDelete("experts")(svc.handleArchivePack))
	})
	return nil
}

// MCPTools implements modules.Module.
func (Module) MCPTools() []mcpx.ToolDef {
	return []mcpx.ToolDef{
		{
			Name:        "experts.read",
			Description: "搜索专家包或获取安装清单",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword":  map[string]any{"type": "string"},
					"slug":     map[string]any{"type": "string"},
					"manifest": map[string]any{"type": "boolean"},
					"page":     map[string]any{"type": "integer"},
					"pageSize": map[string]any{"type": "integer"},
				},
			},
			Group: "experts",
		},
		{
			Name:        "experts.write",
			Description: "创建专家包并构建版本",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action":  map[string]any{"type": "string", "enum": []any{"create", "build"}},
					"slug":    map[string]any{"type": "string"},
					"name":    map[string]any{"type": "string"},
					"members": map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
				},
				"required": []any{"action"},
			},
			Write: true,
			Group: "experts",
		},
		{
			Name:        "experts.delete",
			Description: "归档专家包",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug": map[string]any{"type": "string"},
				},
				"required": []any{"slug"},
			},
			Write:  true,
			Delete: true,
			Group:  "experts",
		},
	}
}

// Health implements modules.Module.
func (Module) Health(ctx context.Context, deps *modules.Deps) error { return deps.DB.Ping(ctx) }
