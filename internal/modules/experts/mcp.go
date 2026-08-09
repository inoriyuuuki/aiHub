package experts

import (
	"context"
	"strconv"

	"github.com/aihub/aihub/internal/mcpx"
	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

func (s *Service) mcpTools() []mcpx.ToolDef {
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
			Group:   "experts",
			Handler: s.mcpReadExperts,
		},
		{
			Name:        "experts.write",
			Description: "创建专家包并构建版本",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{"type": "string", "enum": []any{"create", "build"}},
					"slug":   map[string]any{"type": "string"},
					"name":   map[string]any{"type": "string"},
				},
				"required": []any{"action"},
			},
			Write:   true,
			Group:   "experts",
			Handler: s.mcpWriteExpert,
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
			Write:   false,
			Delete:  true,
			Group:   "experts",
			Handler: s.mcpDeleteExpert,
		},
	}
}

func (s *Service) mcpReadExperts(ctx context.Context, args map[string]any) (any, error) {
	if manifest, _ := args["manifest"].(bool); manifest {
		slug, _ := args["slug"].(string)
		if slug == "" {
			return nil, httpx.ErrBadRequest("获取安装清单需要 slug")
		}
		var id int64
		var cur *int64
		if err := s.db.QueryRow(ctx, `SELECT id, current_version_id FROM expert_packs WHERE slug=$1 AND status <> 'archived'`, slug).Scan(&id, &cur); err == pgx.ErrNoRows {
			return nil, httpx.ErrNotFound("专家包不存在")
		} else if err != nil {
			return nil, err
		}
		if cur == nil {
			return nil, httpx.ErrConflict("专家包没有已构建版本")
		}
		v, err := s.getVersion(ctx, id, *cur)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"pack":        map[string]any{"slug": slug, "name": v.Manifest.Pack.Name, "version": v.Version, "sha256": v.SHA256},
			"manifest":    v.Manifest,
			"downloadUrl": "GET /api/v1/expert-packs/" + strconv.FormatInt(id, 10) + "/versions/" + strconv.Itoa(v.Version) + "/download",
		}, nil
	}
	if slug, _ := args["slug"].(string); slug != "" {
		var id int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM expert_packs WHERE slug=$1 AND status <> 'archived'`, slug).Scan(&id); err != nil {
			return nil, httpx.ErrNotFound("专家包不存在: " + slug)
		}
		d, aerr := s.getPack(ctx, id)
		if aerr != nil {
			return nil, aerr
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
	where := `WHERE status <> 'archived'`
	qargs := []any{}
	arg := func(v any) string {
		qargs = append(qargs, v)
		return "$" + strconv.Itoa(len(qargs))
	}
	if kw, _ := args["keyword"].(string); kw != "" {
		where += ` AND (name ILIKE ` + arg("%"+kw+"%") + ` OR slug ILIKE ` + arg("%"+kw+"%") + `)`
	}
	var total int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM expert_packs `+where, qargs...).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, project_id, name, slug, description, domain, responsibility, usage, status, current_version_id, created_at, updated_at
		FROM expert_packs `+where+` ORDER BY updated_at DESC LIMIT `+arg(size)+` OFFSET `+arg((page-1)*size), qargs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []packDTO{}
	for rows.Next() {
		var d packDTO
		var cur *int64
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.Name, &d.Slug, &d.Description, &d.Domain, &d.Responsibility, &d.Usage, &d.Status, &cur, &d.CreatedAt, &d.UpdatedAt); err != nil {
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

func (s *Service) mcpWriteExpert(ctx context.Context, args map[string]any) (any, error) {
	action, _ := args["action"].(string)
	switch action {
	case "create":
		name, _ := args["name"].(string)
		slug, _ := args["slug"].(string)
		if name == "" || slug == "" {
			return nil, httpx.ErrUnprocessable("create 需要 name 和 slug")
		}
		var id int64
		err := s.db.QueryRow(ctx, `
			INSERT INTO expert_packs (name, slug) VALUES ($1,$2) RETURNING id`, name, slug).Scan(&id)
		if err != nil {
			return nil, err
		}
		d, aerr := s.getPack(ctx, id)
		if aerr != nil {
			return nil, aerr
		}
		return d, nil
	case "build":
		slug, _ := args["slug"].(string)
		var id int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM expert_packs WHERE slug=$1`, slug).Scan(&id); err != nil {
			return nil, httpx.ErrNotFound("专家包不存在: " + slug)
		}
		return s.buildPack(ctx, id, "")
	}
	return nil, httpx.ErrUnprocessable("未知 action")
}

func (s *Service) mcpDeleteExpert(ctx context.Context, args map[string]any) (any, error) {
	slug, _ := args["slug"].(string)
	if slug == "" {
		return nil, httpx.ErrUnprocessable("缺少 slug")
	}
	tag, err := s.db.Exec(ctx, `UPDATE expert_packs SET status='archived', updated_at=now() WHERE slug=$1 AND status <> 'archived'`, slug)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, httpx.ErrNotFound("专家包不存在或已归档")
	}
	return map[string]bool{"ok": true}, nil
}
