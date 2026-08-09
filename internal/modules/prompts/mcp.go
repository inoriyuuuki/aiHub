package prompts

import (
	"context"
	"strconv"

	"github.com/aihub/aihub/internal/mcpx"
	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

// mcpTools returns prompts MCP tools with handlers.
func (s *Service) mcpTools() []mcpx.ToolDef {
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
					"slug":     map[string]any{"type": "string"},
					"version":  map[string]any{"type": "integer"},
					"page":     map[string]any{"type": "integer"},
					"pageSize": map[string]any{"type": "integer"},
				},
			},
			Group:   "prompts",
			Handler: s.mcpReadPrompts,
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
			Group:   "prompts",
			Handler: s.mcpRenderPrompt,
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
			Write:   true,
			Group:   "prompts",
			Handler: s.mcpWritePrompt,
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
			Write:   false,
			Delete:  true,
			Group:   "prompts",
			Handler: s.mcpDeletePrompt,
		},
	}
}

func (s *Service) mcpReadPrompts(ctx context.Context, args map[string]any) (any, error) {
	// If a slug is provided, resolve a single prompt (project-first).
	if slug, ok := args["slug"].(string); ok && slug != "" {
		project, _ := args["project"].(string)
		var id int64
		err := s.db.QueryRow(ctx, `
			SELECT p.id FROM prompts p
			LEFT JOIN projects pr ON pr.id = p.project_id
			WHERE p.slug=$1 AND p.status <> 'archived' AND ($2='' OR pr.slug=$2)
			ORDER BY (p.project_id IS NOT NULL) DESC LIMIT 1`, slug, project).Scan(&id)
		if err == pgx.ErrNoRows {
			return nil, httpx.ErrNotFound("提示词不存在: " + slug)
		}
		if err != nil {
			return nil, err
		}
		d, aerr := s.getPrompt(ctx, id)
		if aerr != nil {
			return nil, aerr
		}
		if v, ok := args["version"].(float64); ok {
			ver, aerr := s.getVersionByNumber(ctx, id, int(v))
			if aerr != nil {
				return nil, aerr
			}
			return map[string]any{"prompt": d, "version": ver}, nil
		}
		return d, nil
	}
	page, size := 1, 20
	if p, ok := args["page"].(float64); ok && p > 0 {
		page = int(p)
	}
	if ps, ok := args["pageSize"].(float64); ok && ps > 0 {
		size = int(ps)
	}
	if size > 100 {
		size = 100
	}
	where := `WHERE p.status <> 'archived'`
	qargs := []any{}
	arg := func(v any) string {
		qargs = append(qargs, v)
		return "$" + strconv.Itoa(len(qargs))
	}
	if kw, _ := args["keyword"].(string); kw != "" {
		where += ` AND (p.title ILIKE ` + arg("%"+kw+"%") + ` OR p.slug ILIKE ` + arg("%"+kw+"%") + `)`
	}
	if cat, _ := args["category"].(string); cat != "" {
		where += ` AND p.category_id = (SELECT id FROM prompt_categories WHERE slug=` + arg(cat) + ` LIMIT 1)`
	}
	if tag, _ := args["tag"].(string); tag != "" {
		where += ` AND ` + arg(tag) + ` = ANY(p.tags)`
	}
	if project, _ := args["project"].(string); project != "" {
		where += ` AND (p.project_id IS NULL OR p.project_id IN (SELECT id FROM projects WHERE slug=` + arg(project) + `))`
	}
	if status, _ := args["status"].(string); status != "" {
		where += ` AND p.status = ` + arg(status)
	}
	var total int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM prompts p `+where, qargs...).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT p.id, p.project_id, p.category_id, pc.name, p.slug, p.title, p.description, p.tags, p.status,
		       p.current_version_id, p.created_at, p.updated_at
		FROM prompts p LEFT JOIN prompt_categories pc ON pc.id = p.category_id
		`+where+` ORDER BY p.updated_at DESC LIMIT `+arg(size)+` OFFSET `+arg((page-1)*size), qargs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []promptDTO{}
	for rows.Next() {
		var d promptDTO
		var cur *int64
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.CategoryID, &d.CategoryName, &d.Slug, &d.Title, &d.Description, &d.Tags, &d.Status, &cur, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		if cur != nil {
			if v, err := s.getVersion(ctx, d.ID, *cur); err == nil {
				d.CurrentVersion = v
			}
		}
		items = append(items, d)
	}
	return httpx.PageOf(items, total, httpx.Page{Page: page, PageSize: size}), nil
}

func (s *Service) mcpRenderPrompt(ctx context.Context, args map[string]any) (any, error) {
	slug, _ := args["slug"].(string)
	project, _ := args["project"].(string)
	values, _ := args["values"].(map[string]any)
	var id int64
	err := s.db.QueryRow(ctx, `
		SELECT p.id FROM prompts p
		LEFT JOIN projects pr ON pr.id = p.project_id
		WHERE p.slug=$1 AND p.status <> 'archived' AND ($2='' OR pr.slug=$2)
		ORDER BY (p.project_id IS NOT NULL) DESC LIMIT 1`, slug, project).Scan(&id)
	if err == pgx.ErrNoRows {
		return nil, httpx.ErrNotFound("提示词不存在: " + slug)
	}
	if err != nil {
		return nil, err
	}
	var content map[string]any
	if v, ok := args["version"].(float64); ok {
		ver, aerr := s.getVersionByNumber(ctx, id, int(v))
		if aerr != nil {
			return nil, aerr
		}
		content = ver.Content
	} else {
		content, err = s.getDraftContent(ctx, id)
		if err != nil {
			var cur *int64
			_ = s.db.QueryRow(ctx, `SELECT current_version_id FROM prompts WHERE id=$1`, id).Scan(&cur)
			if cur == nil {
				return nil, httpx.ErrNotFound("没有可渲染的内容")
			}
			ver, aerr := s.getVersion(ctx, id, *cur)
			if aerr != nil {
				return nil, aerr
			}
			content = ver.Content
		}
	}
	return RenderContent(content, values), nil
}

func (s *Service) mcpWritePrompt(ctx context.Context, args map[string]any) (any, error) {
	action, _ := args["action"].(string)
	switch action {
	case "create", "update":
		slug, _ := args["slug"].(string)
		title, _ := args["title"].(string)
		categorySlug, _ := args["category"].(string)
		content, _ := args["content"].(map[string]any)
		if slug == "" || title == "" || categorySlug == "" {
			return nil, httpx.ErrUnprocessable("create/update 需要 slug、title、category")
		}
		var catID int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM prompt_categories WHERE slug=$1 AND archived=false LIMIT 1`, categorySlug).Scan(&catID); err != nil {
			return nil, httpx.ErrUnprocessable("分类不存在: " + categorySlug)
		}
		var id int64
		if action == "update" {
			err := s.db.QueryRow(ctx, `SELECT id FROM prompts WHERE slug=$1 AND status <> 'archived'`, slug).Scan(&id)
			if err == pgx.ErrNoRows {
				return nil, httpx.ErrNotFound("提示词不存在: " + slug)
			}
			in := promptInput{CategoryID: catID, Slug: slug, Title: title, Content: content}
			_, aerr := s.updatePrompt(ctx, id, in)
			if aerr != nil {
				return nil, aerr
			}
		} else {
			in := promptInput{CategoryID: catID, Slug: slug, Title: title, Content: content}
			var aerr *httpx.Error
			id, aerr = s.createPrompt(ctx, in)
			if aerr != nil {
				return nil, aerr
			}
		}
		d, aerr := s.getPrompt(ctx, id)
		if aerr != nil {
			return nil, aerr
		}
		return d, nil
	case "publish":
		slug, _ := args["slug"].(string)
		var id int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM prompts WHERE slug=$1 AND status='draft'`, slug).Scan(&id); err != nil {
			return nil, httpx.ErrNotFound("草稿不存在: " + slug)
		}
		d, aerr := s.publishPrompt(ctx, id, "")
		if aerr != nil {
			return nil, aerr
		}
		return d, nil
	}
	return nil, httpx.ErrUnprocessable("未知 action")
}

func (s *Service) mcpDeletePrompt(ctx context.Context, args map[string]any) (any, error) {
	slug, _ := args["slug"].(string)
	if slug == "" {
		return nil, httpx.ErrUnprocessable("缺少 slug")
	}
	tag, err := s.db.Exec(ctx, `UPDATE prompts SET status='archived', updated_at=now() WHERE slug=$1 AND status <> 'archived'`, slug)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, httpx.ErrNotFound("提示词不存在或已归档")
	}
	return map[string]bool{"ok": true}, nil
}
