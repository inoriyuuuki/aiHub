package prompts

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/aihub/aihub/internal/platform/db"
	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

type categoryDTO struct {
	ID          int64      `json:"id"`
	ParentID    *int64     `json:"parentId,omitempty"`
	ProjectID   *int64     `json:"projectId,omitempty"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	Icon        string     `json:"icon"`
	Description string     `json:"description"`
	SortOrder   int        `json:"sortOrder"`
	Archived    bool       `json:"archived"`
	SchemaID    *int64     `json:"schemaId,omitempty"`
	Schema      *schemaDTO `json:"schema,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type schemaDTO struct {
	ID        int64          `json:"id"`
	Version   int            `json:"version"`
	Schema    map[string]any `json:"schema"`
	CreatedAt time.Time      `json:"createdAt"`
}

type categoryInput struct {
	ParentID    *int64 `json:"parentId"`
	ProjectID   *int64 `json:"projectId"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Icon        string `json:"icon"`
	Description string `json:"description"`
	SortOrder   *int   `json:"sortOrder"`
}

func (s *Service) handleListCategories(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	projectID := parseNullableInt64(q.Get("projectId"))
	includeArchived := q.Get("archived") == "true"
	rows, err := s.db.Query(r.Context(), `
		SELECT id, parent_id, project_id, name, slug, icon, description, sort_order, archived,
		       (SELECT id FROM prompt_schemas ps WHERE ps.category_id = prompt_categories.id ORDER BY version DESC LIMIT 1) AS schema_id,
		       created_at, updated_at
		FROM prompt_categories
		WHERE ($1::bigint IS NULL OR project_id = $1)
		  AND ($2::bool OR NOT archived)
		ORDER BY sort_order, name`, projectID, includeArchived)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer rows.Close()
	out := []categoryDTO{}
	for rows.Next() {
		var d categoryDTO
		if err := rows.Scan(&d.ID, &d.ParentID, &d.ProjectID, &d.Name, &d.Slug, &d.Icon, &d.Description, &d.SortOrder, &d.Archived, &d.SchemaID, &d.CreatedAt, &d.UpdatedAt); err != nil {
			httpx.WriteError(w, err)
			return
		}
		out = append(out, d)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Service) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
	var in categoryInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if in.Name == "" || in.Slug == "" {
		httpx.WriteError(w, httpx.ErrUnprocessable("名称和 slug 不能为空"))
		return
	}
	sortOrder := 0
	if in.SortOrder != nil {
		sortOrder = *in.SortOrder
	}
	var id int64
	err := s.db.QueryRow(r.Context(), `
		INSERT INTO prompt_categories (parent_id, project_id, name, slug, icon, description, sort_order)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		in.ParentID, in.ProjectID, in.Name, in.Slug, in.Icon, in.Description, sortOrder).Scan(&id)
	if db.IsUniqueViolation(err) {
		httpx.WriteError(w, httpx.ErrConflict("slug 已存在"))
		return
	}
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	d, aerr := s.getCategory(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusCreated, d)
}

func (s *Service) handleGetCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	d, aerr := s.getCategory(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (s *Service) handleUpdateCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	var in categoryInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	sortOrder := 0
	if in.SortOrder != nil {
		sortOrder = *in.SortOrder
	}
	tag, err := s.db.Exec(r.Context(), `
		UPDATE prompt_categories SET name=$1, slug=$2, icon=$3, description=$4, sort_order=$5, updated_at=now()
		WHERE id=$6`, in.Name, in.Slug, in.Icon, in.Description, sortOrder, id)
	if err != nil {
		if db.IsUniqueViolation(err) {
			httpx.WriteError(w, httpx.ErrConflict("slug 已存在"))
			return
		}
		httpx.WriteError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteError(w, httpx.ErrNotFound("分类不存在"))
		return
	}
	d, aerr := s.getCategory(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (s *Service) handleArchiveCategory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	tag, err := s.db.Exec(r.Context(), `UPDATE prompt_categories SET archived=true, updated_at=now() WHERE id=$1 AND archived=false`, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteError(w, httpx.ErrNotFound("分类不存在或已归档"))
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleCreateSchema creates a new immutable schema version for a category.
func (s *Service) handleCreateSchema(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	var req struct {
		Schema map[string]any `json:"schema"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := ValidateSchema(req.Schema); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	var nextVersion int
	if err := tx.QueryRow(r.Context(), `SELECT COALESCE(MAX(version),0)+1 FROM prompt_schemas WHERE category_id=$1`, id).Scan(&nextVersion); err != nil {
		httpx.WriteError(w, err)
		return
	}
	var d schemaDTO
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO prompt_schemas (category_id, version, schema) VALUES ($1,$2,$3)
		RETURNING id, version, schema, created_at`, id, nextVersion, req.Schema).
		Scan(&d.ID, &d.Version, &d.Schema, &d.CreatedAt); err != nil {
		if db.IsUniqueViolation(err) {
			httpx.WriteError(w, httpx.ErrConflict("该版本已存在"))
			return
		}
		httpx.WriteError(w, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE prompt_categories SET updated_at=now() WHERE id=$1`, id); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, d)
}

func (s *Service) handleListSchemas(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, version, schema, created_at FROM prompt_schemas WHERE category_id=$1 ORDER BY version DESC`, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer rows.Close()
	out := []schemaDTO{}
	for rows.Next() {
		var d schemaDTO
		if err := rows.Scan(&d.ID, &d.Version, &d.Schema, &d.CreatedAt); err != nil {
			httpx.WriteError(w, err)
			return
		}
		out = append(out, d)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Service) getCategory(ctx context.Context, id int64) (categoryDTO, *httpx.Error) {
	var d categoryDTO
	err := s.db.QueryRow(ctx, `
		SELECT c.id, c.parent_id, c.project_id, c.name, c.slug, c.icon, c.description, c.sort_order, c.archived,
		       (SELECT id FROM prompt_schemas ps WHERE ps.category_id = c.id ORDER BY version DESC LIMIT 1),
		       c.created_at, c.updated_at
		FROM prompt_categories c WHERE c.id=$1`, id).
		Scan(&d.ID, &d.ParentID, &d.ProjectID, &d.Name, &d.Slug, &d.Icon, &d.Description, &d.SortOrder, &d.Archived, &d.SchemaID, &d.CreatedAt, &d.UpdatedAt)
	if err == pgx.ErrNoRows {
		return d, httpx.ErrNotFound("分类不存在")
	}
	if err != nil {
		return d, httpx.WrapError(err)
	}
	if d.SchemaID != nil {
		var sc schemaDTO
		if err := s.db.QueryRow(ctx, `SELECT id, version, schema, created_at FROM prompt_schemas WHERE id=$1`, *d.SchemaID).Scan(&sc.ID, &sc.Version, &sc.Schema, &sc.CreatedAt); err == nil {
			d.Schema = &sc
		}
	}
	return d, nil
}

func parseNullableInt64(v string) *int64 {
	if v == "" {
		return nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}
