package core

import (
	"context"

	"github.com/aihub/aihub/internal/mcpx"
	"github.com/aihub/aihub/internal/platform/httpx"
)

// mcpTools returns the core module's MCP tools with handlers.
func (s *Service) mcpTools() []mcpx.ToolDef {
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
			Group:   "projects",
			Handler: s.mcpListProjects,
		},
	}
}

func (s *Service) mcpListProjects(ctx context.Context, args map[string]any) (any, error) {
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
	keyword, _ := args["keyword"].(string)
	archived, _ := args["archived"].(bool)
	p := httpx.Page{Page: page, PageSize: size, Offset: (page - 1) * size}
	items, total, err := s.queryProjects(ctx, keyword, archived, &p)
	if err != nil {
		return nil, err
	}
	return httpx.PageOf(items, total, p), nil
}
