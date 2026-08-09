// Package skills implements Skill metadata, versioned packages and install
// manifests for Codex.
package skills

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

// Module implements the skills module.
type Module struct{}

// ID implements modules.Module.
func (Module) ID() string { return "skills" }

// Version implements modules.Module.
func (Module) Version() string { return "1.0.0" }

// DependsOn implements modules.Module.
func (Module) DependsOn() []string { return []string{"core"} }

// Migrations implements modules.Module.
func (Module) Migrations() []db.Migration {
	return []db.Migration{
		{ID: "20260809003_skills", FS: &migrationsFS, File: "migrations/20260809003_skills.sql"},
	}
}

// Service holds skills module dependencies.
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
		r.Handle("GET", "/skills", auth.RequireAuth(svc.handleListSkills))
		r.Handle("POST", "/skills/upload", auth.RequireWrite("skills")(svc.handleUploadSkill))
		r.Handle("GET", "/skills/resolve", auth.RequireAuth(svc.handleGetSkillBySlug))
		r.Handle("GET", "/skills/install-manifest", auth.RequireAuth(svc.handleInstallManifest))
		r.Handle("GET", "/skills/{id}", auth.RequireAuth(svc.handleGetSkill))
		r.Handle("GET", "/skills/{id}/versions", auth.RequireAuth(svc.handleListVersions))
		r.Handle("POST", "/skills/{id}/versions", auth.RequireWrite("skills")(svc.handleAddVersion))
		r.Handle("GET", "/skills/{id}/versions/{v}/download", auth.RequireAuth(svc.handleDownload))
		r.Handle("DELETE", "/skills/{id}", auth.RequireDelete("skills")(svc.handleArchiveSkill))
	})
	return nil
}

// MCPTools implements modules.Module.
func (Module) MCPTools() []mcpx.ToolDef {
	return []mcpx.ToolDef{
		{
			Name:        "skills.read",
			Description: "搜索 Skill 或获取安装清单",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword":  map[string]any{"type": "string"},
					"slug":     map[string]any{"type": "string"},
					"project":  map[string]any{"type": "string"},
					"manifest": map[string]any{"type": "boolean"},
					"page":     map[string]any{"type": "integer"},
					"pageSize": map[string]any{"type": "integer"},
				},
			},
			Group: "skills",
		},
		{
			Name:        "skills.write",
			Description: "发布 Skill 新版本",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug":      map[string]any{"type": "string"},
					"changelog": map[string]any{"type": "string"},
					"zipBase64": map[string]any{"type": "string"},
				},
				"required": []any{"slug", "zipBase64"},
			},
			Write: true,
			Group: "skills",
		},
		{
			Name:        "skills.delete",
			Description: "归档 Skill",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug": map[string]any{"type": "string"},
				},
				"required": []any{"slug"},
			},
			Write:  true,
			Delete: true,
			Group:  "skills",
		},
	}
}

// Health implements modules.Module.
func (Module) Health(ctx context.Context, deps *modules.Deps) error { return deps.DB.Ping(ctx) }
