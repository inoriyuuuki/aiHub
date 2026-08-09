package core

import (
	"net/http"
	"strconv"
	"time"

	"github.com/aihub/aihub/internal/platform/db"
	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

// projectDTO is the public project representation.
type projectDTO struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Scope       string    `json:"scope"`
	Archived    bool      `json:"archived"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type projectInput struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Scope       string `json:"scope"`
}

func (in *projectInput) validate() *httpx.Error {
	if in.Name == "" {
		return httpx.ErrUnprocessable("项目名称不能为空")
	}
	if in.Slug == "" {
		return httpx.ErrUnprocessable("项目 slug 不能为空")
	}
	if in.Scope == "" {
		in.Scope = "global"
	}
	if in.Scope != "global" && in.Scope != "project" {
		return httpx.ErrUnprocessable("scope 必须是 global 或 project")
	}
	return nil
}

// HandleListProjects lists projects with keyword and archived filters.
func (s *Service) HandleListProjects(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r)
	q := r.URL.Query()
	keyword := q.Get("keyword")
	archived := q.Get("archived")

	where := `WHERE true`
	args := []any{}
	if keyword != "" {
		args = append(args, "%"+keyword+"%")
		where += ` AND (name ILIKE $` + strconv.Itoa(len(args)) + ` OR slug ILIKE $` + strconv.Itoa(len(args)) + `)`
	}
	if archived == "true" {
		where += ` AND archived = true`
	} else if archived != "true" {
		where += ` AND archived = false`
	}
	var total int
	if err := s.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM projects `+where, args...).Scan(&total); err != nil {
		httpx.WriteError(w, err)
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, name, slug, description, scope, archived, created_at, updated_at
		FROM projects `+where+` ORDER BY created_at DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer rows.Close()
	items := []projectDTO{}
	for rows.Next() {
		var d projectDTO
		if err := rows.Scan(&d.ID, &d.Name, &d.Slug, &d.Description, &d.Scope, &d.Archived, &d.CreatedAt, &d.UpdatedAt); err != nil {
			httpx.WriteError(w, err)
			return
		}
		items = append(items, d)
	}
	httpx.JSON(w, http.StatusOK, httpx.PageOf(items, total, p))
}

// HandleCreateProject creates a project.
func (s *Service) HandleCreateProject(w http.ResponseWriter, r *http.Request) {
	var in projectInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := in.validate(); err != nil {
		httpx.WriteError(w, err)
		return
	}
	var d projectDTO
	err := s.db.QueryRow(r.Context(), `
		INSERT INTO projects (name, slug, description, scope)
		VALUES ($1,$2,$3,$4)
		RETURNING id, name, slug, description, scope, archived, created_at, updated_at`,
		in.Name, in.Slug, in.Description, in.Scope).
		Scan(&d.ID, &d.Name, &d.Slug, &d.Description, &d.Scope, &d.Archived, &d.CreatedAt, &d.UpdatedAt)
	if db.IsUniqueViolation(err) {
		httpx.WriteError(w, httpx.ErrConflict("slug 已存在"))
		return
	}
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, d)
}

// HandleGetProject returns a single project.
func (s *Service) HandleGetProject(w http.ResponseWriter, r *http.Request) {
	d, err := s.getProject(r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

// HandleUpdateProject patches a project.
func (s *Service) HandleUpdateProject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	var in projectInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := in.validate(); err != nil {
		httpx.WriteError(w, err)
		return
	}
	var d projectDTO
	err = s.db.QueryRow(r.Context(), `
		UPDATE projects SET name=$1, slug=$2, description=$3, scope=$4, updated_at=now()
		WHERE id=$5 RETURNING id, name, slug, description, scope, archived, created_at, updated_at`,
		in.Name, in.Slug, in.Description, in.Scope, id).
		Scan(&d.ID, &d.Name, &d.Slug, &d.Description, &d.Scope, &d.Archived, &d.CreatedAt, &d.UpdatedAt)
	if err == pgx.ErrNoRows {
		httpx.WriteError(w, httpx.ErrNotFound("项目不存在"))
		return
	}
	if db.IsUniqueViolation(err) {
		httpx.WriteError(w, httpx.ErrConflict("slug 已存在"))
		return
	}
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

// HandleArchiveProject archives (soft-deletes) a project.
func (s *Service) HandleArchiveProject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	tag, err := s.db.Exec(r.Context(), `UPDATE projects SET archived=true, updated_at=now() WHERE id=$1 AND archived=false`, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteError(w, httpx.ErrNotFound("项目不存在或已归档"))
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Service) getProject(r *http.Request) (projectDTO, *httpx.Error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return projectDTO{}, httpx.ErrBadRequest("无效的 id")
	}
	var d projectDTO
	err = s.db.QueryRow(r.Context(), `
		SELECT id, name, slug, description, scope, archived, created_at, updated_at
		FROM projects WHERE id=$1`, id).
		Scan(&d.ID, &d.Name, &d.Slug, &d.Description, &d.Scope, &d.Archived, &d.CreatedAt, &d.UpdatedAt)
	if err == pgx.ErrNoRows {
		return projectDTO{}, httpx.ErrNotFound("项目不存在")
	}
	if err != nil {
		return projectDTO{}, httpx.WrapError(err)
	}
	return d, nil
}
