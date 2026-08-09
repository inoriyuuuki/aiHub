package mcpcat

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aihub/aihub/internal/mcpx"
	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

func (s *Service) mcpTools() []mcpx.ToolDef {
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
			Group:   "mcp_catalog",
			Handler: s.mcpReadCatalog,
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
			Write:   true,
			Group:   "mcp_catalog",
			Handler: s.mcpWriteCatalog,
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
			Write:   true,
			Delete:  true,
			Group:   "mcp_catalog",
			Handler: s.mcpDeleteCatalog,
		},
	}
}

func (s *Service) mcpReadCatalog(ctx context.Context, args map[string]any) (any, error) {
	if manifest, _ := args["manifest"].(bool); manifest {
		profile, _ := args["profile"].(string)
		if profile == "" {
			return nil, httpx.ErrBadRequest("获取安装清单需要 profile")
		}
		var pid int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM mcp_profiles WHERE slug=$1 AND status <> 'archived'`, profile).Scan(&pid); err == pgx.ErrNoRows {
			return nil, httpx.ErrNotFound("Profile 不存在")
		} else if err != nil {
			return nil, err
		}
		prof, aerr := s.getProfile(ctx, pid)
		if aerr != nil {
			return nil, aerr
		}
		manifestOut := installManifest{Profile: prof, ManagedKey: "aihub"}
		enabledSet := map[string]bool{}
		disabledSet := map[string]bool{}
		for _, it := range prof.Items {
			server := mcpServerConfig{
				Name:          it.DefinitionSlug,
				Type:          it.Transport,
				EnabledTools:  it.EnabledTools,
				DisabledTools: it.DisabledTools,
			}
			if cmd, _ := it.Config["command"].(string); cmd != "" {
				server.Command = cmd
			}
			if args, ok := it.Config["args"].([]any); ok {
				for _, a := range args {
					if as, ok := a.(string); ok {
						server.Args = append(server.Args, as)
					}
				}
			}
			if u, _ := it.Config["url"].(string); u != "" {
				server.URL = u
			}
			if wd, _ := it.Config["workdir"].(string); wd != "" {
				server.Workdir = wd
			}
			server.Env = it.EnvVars
			for _, t := range it.EnabledTools {
				enabledSet[t] = true
			}
			for _, t := range it.DisabledTools {
				disabledSet[t] = true
			}
			manifestOut.MCPServers = append(manifestOut.MCPServers, server)
		}
		for t := range enabledSet {
			manifestOut.EnabledTools = append(manifestOut.EnabledTools, t)
		}
		for t := range disabledSet {
			manifestOut.DisabledTools = append(manifestOut.DisabledTools, t)
		}
		return manifestOut, nil
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
	// Profiles
	var total int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM mcp_profiles `+where, qargs...).Scan(&total); err != nil {
		return nil, err
	}
	qargs2 := append([]any{}, qargs...)
	qargs2 = append(qargs2, size, (page-1)*size)
	rows, err := s.db.Query(ctx, `
		SELECT id, name, slug, description, scope, project_id, status, created_at, updated_at
		FROM mcp_profiles `+where+` ORDER BY updated_at DESC LIMIT `+arg(size)+` OFFSET `+arg((page-1)*size), qargs2...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []profileDTO{}
	for rows.Next() {
		var d profileDTO
		if err := rows.Scan(&d.ID, &d.Name, &d.Slug, &d.Description, &d.Scope, &d.ProjectID, &d.Status, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return httpx.PageOf(items, total, httpx.Page{Page: page, PageSize: size}), nil
}

func (s *Service) mcpWriteCatalog(ctx context.Context, args map[string]any) (any, error) {
	action, _ := args["action"].(string)
	name, _ := args["name"].(string)
	slug, _ := args["slug"].(string)
	transport, _ := args["transport"].(string)
	switch action {
	case "create_definition":
		if name == "" || slug == "" {
			return nil, httpx.ErrUnprocessable("create_definition 需要 name 和 slug")
		}
		if transport == "" {
			transport = "stdio"
		}
		var id int64
		err := s.db.QueryRow(ctx, `
			INSERT INTO mcp_definitions (name, slug, transport) VALUES ($1,$2,$3) RETURNING id`,
			name, slug, transport).Scan(&id)
		if err != nil {
			return nil, err
		}
		d, aerr := s.getDefinition(ctx, id)
		if aerr != nil {
			return nil, aerr
		}
		return d, nil
	case "create_profile":
		if name == "" || slug == "" {
			return nil, httpx.ErrUnprocessable("create_profile 需要 name 和 slug")
		}
		var id int64
		err := s.db.QueryRow(ctx, `
			INSERT INTO mcp_profiles (name, slug) VALUES ($1,$2) RETURNING id`, name, slug).Scan(&id)
		if err != nil {
			return nil, err
		}
		d, aerr := s.getProfile(ctx, id)
		if aerr != nil {
			return nil, aerr
		}
		return d, nil
	}
	return nil, httpx.ErrUnprocessable("未知 action")
}

func (s *Service) mcpDeleteCatalog(ctx context.Context, args map[string]any) (any, error) {
	kind, _ := args["kind"].(string)
	slug, _ := args["slug"].(string)
	if slug == "" {
		return nil, httpx.ErrUnprocessable("缺少 slug")
	}
	var tag pgconn.CommandTag
	var err error
	switch kind {
	case "definition":
		tag, err = s.db.Exec(ctx, `UPDATE mcp_definitions SET status='archived', updated_at=now() WHERE slug=$1 AND status <> 'archived'`, slug)
	case "profile":
		tag, err = s.db.Exec(ctx, `UPDATE mcp_profiles SET status='archived', updated_at=now() WHERE slug=$1 AND status <> 'archived'`, slug)
	default:
		return nil, httpx.ErrUnprocessable("kind 必须是 definition 或 profile")
	}
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, httpx.ErrNotFound("资源不存在或已归档")
	}
	return map[string]bool{"ok": true}, nil
}
