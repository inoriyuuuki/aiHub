// Package prompts implements dynamic prompt categories, versioned schemas,
// prompt versions and MinIO-backed assets.
package prompts

import (
	"context"
	"embed"
	"log/slog"

	"github.com/aihub/aihub/internal/config"
	"github.com/aihub/aihub/internal/mcpx"
	"github.com/aihub/aihub/internal/modules"
	"github.com/aihub/aihub/internal/platform/db"
	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/aihub/aihub/internal/platform/storage"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Module implements the prompts module.
type Module struct{}

// ID implements modules.Module.
func (Module) ID() string { return "prompts" }

// Version implements modules.Module.
func (Module) Version() string { return "1.0.0" }

// DependsOn implements modules.Module.
func (Module) DependsOn() []string { return []string{"core"} }

// Migrations implements modules.Module.
func (Module) Migrations() []db.Migration {
	return []db.Migration{
		{ID: "20260809002_prompts", FS: &migrationsFS, File: "migrations/20260809002_prompts.sql"},
	}
}

// Service holds prompts module dependencies.
type Service struct {
	db    *db.Pool
	store *storage.Storage
	cfg   *config.Config
	log   *slog.Logger
}

// Register implements modules.Module.
func (Module) Register(r *httpx.Router, deps *modules.Deps) error {
	svc := &Service{db: deps.DB, store: deps.Store, cfg: deps.Cfg, log: deps.Logger}
	for _, t := range svc.mcpTools() {
		if err := deps.MCP.Add(t); err != nil {
			return err
		}
	}
	auth := deps.Auth

	r.Group("/api/v1", func(r *httpx.Router) {
		r.Handle("GET", "/prompt-categories", auth.RequireAuth(svc.handleListCategories))
		r.Handle("POST", "/prompt-categories", auth.RequireWrite("prompts")(svc.handleCreateCategory))
		r.Handle("GET", "/prompt-categories/{id}", auth.RequireAuth(svc.handleGetCategory))
		r.Handle("PATCH", "/prompt-categories/{id}", auth.RequireWrite("prompts")(svc.handleUpdateCategory))
		r.Handle("DELETE", "/prompt-categories/{id}", auth.RequireDelete("prompts")(svc.handleArchiveCategory))
		r.Handle("POST", "/prompt-categories/{id}/schemas", auth.RequireWrite("prompts")(svc.handleCreateSchema))
		r.Handle("GET", "/prompt-categories/{id}/schemas", auth.RequireAuth(svc.handleListSchemas))

		r.Handle("GET", "/prompts", auth.RequireAuth(svc.handleListPrompts))
		r.Handle("POST", "/prompts", auth.RequireWrite("prompts")(svc.handleCreatePrompt))
		r.Handle("GET", "/prompts/resolve", auth.RequireAuth(svc.handleGetPromptBySlug))
		r.Handle("GET", "/prompts/{id}", auth.RequireAuth(svc.handleGetPrompt))
		r.Handle("PATCH", "/prompts/{id}", auth.RequireWrite("prompts")(svc.handleUpdatePrompt))
		r.Handle("POST", "/prompts/{id}/publish", auth.RequireWrite("prompts")(svc.handlePublish))
		r.Handle("GET", "/prompts/{id}/versions", auth.RequireAuth(svc.handleListVersions))
		r.Handle("GET", "/prompts/{id}/versions/{v}", auth.RequireAuth(svc.handleGetVersion))
		r.Handle("GET", "/prompts/{id}/versions/{v}/diff", auth.RequireAuth(svc.handleDiff))
		r.Handle("POST", "/prompts/{id}/rollback", auth.RequireWrite("prompts")(svc.handleRollback))
		r.Handle("POST", "/prompts/{id}/render", auth.RequireAuth(svc.handleRender))
		r.Handle("DELETE", "/prompts/{id}", auth.RequireDelete("prompts")(svc.handleArchivePrompt))

		r.Handle("POST", "/assets/presign", auth.RequireWrite("prompts")(svc.handlePresign))
		r.Handle("POST", "/assets/confirm", auth.RequireWrite("prompts")(svc.handleConfirm))
		r.Handle("GET", "/assets/{id}/url", auth.RequireAuth(svc.handleAssetURL))
	})
	return nil
}

// MCPTools implements modules.Module.
func (Module) MCPTools() []mcpx.ToolDef {
	return []mcpx.ToolDef{
		{
			Name:        "prompts.read",
			Description: "搜索提示词或读取指定版本",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword":  map[string]any{"type": "string"},
					"category": map[string]any{"type": "string"},
					"tag":      map[string]any{"type": "string"},
					"project":  map[string]any{"type": "string"},
					"status":   map[string]any{"type": "string"},
					"page":     map[string]any{"type": "integer"},
					"pageSize": map[string]any{"type": "integer"},
				},
			},
			Group: "prompts",
		},
		{
			Name:        "prompts.render",
			Description: "使用变量值渲染提示词模板",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug":    map[string]any{"type": "string"},
					"project": map[string]any{"type": "string"},
					"version": map[string]any{"type": "integer"},
					"values":  map[string]any{"type": "object"},
				},
				"required": []any{"slug", "values"},
			},
			Group: "prompts",
		},
		{
			Name:        "prompts.write",
			Description: "创建或更新提示词草稿，发布新版本",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action":   map[string]any{"type": "string", "enum": []any{"create", "update", "publish"}},
					"slug":     map[string]any{"type": "string"},
					"title":    map[string]any{"type": "string"},
					"category": map[string]any{"type": "string"},
					"content":  map[string]any{"type": "object"},
					"summary":  map[string]any{"type": "string"},
				},
				"required": []any{"action"},
			},
			Write: true,
			Group: "prompts",
		},
		{
			Name:        "prompts.delete",
			Description: "归档提示词",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug": map[string]any{"type": "string"},
				},
				"required": []any{"slug"},
			},
			Write:  true,
			Delete: true,
			Group:  "prompts",
		},
	}
}

// Health implements modules.Module.
func (Module) Health(ctx context.Context, deps *modules.Deps) error { return deps.DB.Ping(ctx) }
