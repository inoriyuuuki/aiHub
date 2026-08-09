package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/aihub/aihub/internal/platform/db"
	"github.com/aihub/aihub/internal/platform/httpx"
)

// seedTemplates creates the three editable category templates on first boot.
func seedTemplates(ctx context.Context, pool *db.Pool) error {
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM prompt_categories`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	templates := []struct {
		name, slug, icon, desc string
		schema                 map[string]any
	}{
		{
			name: "对话提示词", slug: "chat", icon: "💬",
			desc: "多轮对话提示词模板",
			schema: map[string]any{
				"type":  "object",
				"title": "对话提示词",
				"x-aihub-variables": []any{
					map[string]any{"name": "topic", "description": "对话主题", "default": ""},
				},
				"properties": map[string]any{
					"title":  map[string]any{"type": "string", "title": "标题", "x-aihub-ui": "text", "maxLength": float64(200)},
					"system": map[string]any{"type": "string", "title": "系统提示词", "x-aihub-ui": "markdown"},
					"model":  map[string]any{"type": "string", "title": "建议模型", "x-aihub-ui": "model-name"},
					"messages": map[string]any{
						"type": "array", "title": "消息列表", "x-aihub-ui": "repeatable-group",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"role":    map[string]any{"type": "string", "title": "角色", "x-aihub-ui": "select", "enum": []any{"system", "user", "assistant"}},
								"content": map[string]any{"type": "string", "title": "内容", "x-aihub-ui": "markdown"},
							},
							"required": []any{"role"},
						},
					},
					"examples": map[string]any{
						"type": "object", "title": "示例输入输出", "x-aihub-ui": "group",
						"properties": map[string]any{
							"input":  map[string]any{"type": "string", "title": "示例输入", "x-aihub-ui": "textarea"},
							"output": map[string]any{"type": "string", "title": "示例输出", "x-aihub-ui": "textarea"},
						},
					},
					"effectImages": map[string]any{"type": "array", "title": "效果图", "x-aihub-ui": "effect-file", "items": map[string]any{"type": "string"}},
				},
				"required": []any{"title"},
			},
		},
		{
			name: "生图提示词", slug: "image-gen", icon: "🎨",
			desc: "图像生成提示词模板",
			schema: map[string]any{
				"type":  "object",
				"title": "生图提示词",
				"x-aihub-variables": []any{
					map[string]any{"name": "subject", "description": "主体", "default": ""},
					map[string]any{"name": "style", "description": "风格", "default": ""},
				},
				"properties": map[string]any{
					"title":    map[string]any{"type": "string", "title": "标题", "x-aihub-ui": "text"},
					"model":    map[string]any{"type": "string", "title": "模型", "x-aihub-ui": "model-provider"},
					"positive": map[string]any{"type": "string", "title": "正向提示词", "x-aihub-ui": "markdown"},
					"negative": map[string]any{"type": "string", "title": "负向提示词", "x-aihub-ui": "markdown"},
					"parameters": map[string]any{
						"type": "object", "title": "参数", "x-aihub-ui": "group",
						"properties": map[string]any{
							"width":       map[string]any{"type": "integer", "title": "宽度", "x-aihub-ui": "number", "minimum": float64(256), "maximum": float64(4096)},
							"height":      map[string]any{"type": "integer", "title": "高度", "x-aihub-ui": "number", "minimum": float64(256), "maximum": float64(4096)},
							"steps":       map[string]any{"type": "integer", "title": "步数", "x-aihub-ui": "number", "minimum": float64(1), "maximum": float64(150)},
							"cfgScale":    map[string]any{"type": "number", "title": "CFG", "x-aihub-ui": "number"},
							"aspectRatio": map[string]any{"type": "string", "title": "宽高比", "x-aihub-ui": "select", "enum": []any{"1:1", "16:9", "9:16", "4:3", "3:4"}},
						},
					},
					"referenceImages": map[string]any{"type": "array", "title": "参考图", "x-aihub-ui": "image", "items": map[string]any{"type": "string"}},
					"effectImages":    map[string]any{"type": "array", "title": "效果图", "x-aihub-ui": "effect-file", "items": map[string]any{"type": "string"}},
				},
				"required": []any{"title", "positive"},
			},
		},
		{
			name: "代码提示词", slug: "code", icon: "🧑‍💻",
			desc: "代码生成提示词模板",
			schema: map[string]any{
				"type":  "object",
				"title": "代码提示词",
				"properties": map[string]any{
					"title":        map[string]any{"type": "string", "title": "标题", "x-aihub-ui": "text"},
					"language":     map[string]any{"type": "string", "title": "语言", "x-aihub-ui": "select", "enum": []any{"Go", "TypeScript", "Python", "Rust", "Java", "其他"}},
					"framework":    map[string]any{"type": "string", "title": "框架", "x-aihub-ui": "text"},
					"systemPrompt": map[string]any{"type": "string", "title": "系统提示词", "x-aihub-ui": "markdown"},
					"taskPrompt":   map[string]any{"type": "string", "title": "任务提示词", "x-aihub-ui": "markdown"},
					"constraints":  map[string]any{"type": "array", "title": "约束", "x-aihub-ui": "repeatable-group", "items": map[string]any{"type": "object", "properties": map[string]any{"rule": map[string]any{"type": "string", "title": "约束", "x-aihub-ui": "text"}}}},
					"examples":     map[string]any{"type": "object", "title": "示例", "x-aihub-ui": "group", "properties": map[string]any{"code": map[string]any{"type": "string", "title": "示例代码", "x-aihub-ui": "code"}}},
					"attachments":  map[string]any{"type": "array", "title": "附件", "x-aihub-ui": "file", "items": map[string]any{"type": "string"}},
				},
				"required": []any{"title", "taskPrompt"},
			},
		},
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	for _, t := range templates {
		var catID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO prompt_categories (name, slug, icon, description, sort_order)
			VALUES ($1,$2,$3,$4,$5) RETURNING id`, t.name, t.slug, t.icon, t.desc, 0).Scan(&catID); err != nil {
			return err
		}
		schemaJSON, _ := json.Marshal(t.schema)
		if _, err := tx.Exec(ctx, `
			INSERT INTO prompt_schemas (category_id, version, schema)
			VALUES ($1,1,$2)`, catID, schemaJSON); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

var _ = http.StatusOK
var _ = httpx.JSON
