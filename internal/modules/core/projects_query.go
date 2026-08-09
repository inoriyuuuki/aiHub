package core

import (
	"context"
	"strconv"

	"github.com/aihub/aihub/internal/platform/httpx"
)

// queryProjects runs the shared project listing query used by REST and MCP.
func (s *Service) queryProjects(ctx context.Context, keyword string, archived bool, p *httpx.Page) ([]projectDTO, int, error) {
	where := `WHERE archived = $1`
	args := []any{archived}
	if keyword != "" {
		args = append(args, "%"+keyword+"%")
		where += ` AND (name ILIKE $2 OR slug ILIKE $2)`
	}
	var total int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM projects `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, p.PageSize, p.Offset)
	rows, err := s.db.Query(ctx, `
		SELECT id, name, slug, description, scope, archived, created_at, updated_at
		FROM projects `+where+` ORDER BY created_at DESC
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []projectDTO{}
	for rows.Next() {
		var d projectDTO
		if err := rows.Scan(&d.ID, &d.Name, &d.Slug, &d.Description, &d.Scope, &d.Archived, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, d)
	}
	return items, total, nil
}
