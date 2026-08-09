package mcpcat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aihub/aihub/internal/platform/db"
	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/aihub/aihub/internal/platform/slug"
	"github.com/jackc/pgx/v5"
)

type definitionDTO struct {
	ID             int64          `json:"id"`
	ProjectID      *int64         `json:"projectId,omitempty"`
	Name           string         `json:"name"`
	Slug           string         `json:"slug"`
	Description    string         `json:"description"`
	Category       string         `json:"category"`
	Tags           []string       `json:"tags"`
	Transport      string         `json:"transport"`
	Status         string         `json:"status"`
	CurrentVersion *defVersionDTO `json:"currentVersion,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type defVersionDTO struct {
	ID        int64            `json:"id"`
	Version   int              `json:"version"`
	Config    map[string]any   `json:"config"`
	EnvVars   []map[string]any `json:"envVars"`
	Tools     []map[string]any `json:"tools"`
	CreatedAt time.Time        `json:"createdAt"`
}

type definitionInput struct {
	ProjectID   *int64   `json:"projectId"`
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Transport   string   `json:"transport"`
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func sameNullable(a, b sql.NullInt64) bool {
	if !a.Valid || !b.Valid {
		return a.Valid == b.Valid
	}
	return a.Int64 == b.Int64
}

func (s *Service) handleListDefinitions(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r)
	q := r.URL.Query()
	where := `WHERE true`
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if kw := q.Get("keyword"); kw != "" {
		where += ` AND (name ILIKE ` + arg("%"+kw+"%") + ` OR slug ILIKE ` + arg("%"+kw+"%") + `)`
	}
	if transport := q.Get("transport"); transport != "" {
		where += ` AND transport = ` + arg(transport)
	}
	if status := q.Get("status"); status != "" {
		where += ` AND status = ` + arg(status)
	} else {
		where += ` AND status <> 'archived'`
	}
	var total int
	if err := s.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM mcp_definitions `+where, args...).Scan(&total); err != nil {
		httpx.WriteError(w, err)
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, project_id, name, slug, description, category, tags, transport, status, current_version_id, created_at, updated_at
		FROM mcp_definitions `+where+` ORDER BY updated_at DESC LIMIT `+arg(p.PageSize)+` OFFSET `+arg(p.Offset), args...)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer rows.Close()
	items := []definitionDTO{}
	for rows.Next() {
		var d definitionDTO
		var cur *int64
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.Name, &d.Slug, &d.Description, &d.Category, &d.Tags, &d.Transport, &d.Status, &cur, &d.CreatedAt, &d.UpdatedAt); err != nil {
			httpx.WriteError(w, err)
			return
		}
		if cur != nil {
			if v, err := s.getDefVersion(r.Context(), d.ID, *cur); err == nil {
				d.CurrentVersion = v
			}
		}
		items = append(items, d)
	}
	httpx.JSON(w, http.StatusOK, httpx.PageOf(items, total, p))
}

func (s *Service) handleCreateDefinition(w http.ResponseWriter, r *http.Request) {
	var in definitionInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if in.Name == "" || in.Slug == "" {
		httpx.WriteError(w, httpx.ErrUnprocessable("名称和 slug 不能为空"))
		return
	}
	if !slug.Valid(in.Slug) {
		httpx.WriteError(w, httpx.ErrUnprocessable("slug 只能包含小写字母、数字、- 和 _"))
		return
	}
	if in.Transport == "" {
		in.Transport = "stdio"
	}
	if in.Transport != "stdio" && in.Transport != "http" {
		httpx.WriteError(w, httpx.ErrUnprocessable("transport 必须是 stdio 或 http"))
		return
	}
	var id int64
	err := s.db.QueryRow(r.Context(), `
		INSERT INTO mcp_definitions (project_id, name, slug, description, category, tags, transport)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		in.ProjectID, in.Name, in.Slug, in.Description, in.Category, nonNil(in.Tags), in.Transport).Scan(&id)
	if db.IsUniqueViolation(err) {
		httpx.WriteError(w, httpx.ErrConflict("slug 已存在"))
		return
	}
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	d, aerr := s.getDefinition(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusCreated, d)
}

func (s *Service) handleGetDefinition(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	d, aerr := s.getDefinition(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (s *Service) handleUpdateDefinition(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	var in definitionInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if in.Transport == "" {
		in.Transport = "stdio"
	}
	_, err = s.db.Exec(r.Context(), `
		UPDATE mcp_definitions SET project_id=$1, name=$2, slug=$3, description=$4, category=$5, tags=$6, transport=$7, updated_at=now()
		WHERE id=$8`, in.ProjectID, in.Name, in.Slug, in.Description, in.Category, nonNil(in.Tags), in.Transport, id)
	if db.IsUniqueViolation(err) {
		httpx.WriteError(w, httpx.ErrConflict("slug 已存在"))
		return
	}
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	d, aerr := s.getDefinition(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (s *Service) handleListDefVersions(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, version, config, env_vars, tools, created_at
		FROM mcp_definition_versions WHERE definition_id=$1 ORDER BY version DESC`, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer rows.Close()
	out := []defVersionDTO{}
	for rows.Next() {
		var v defVersionDTO
		if err := rows.Scan(&v.ID, &v.Version, &v.Config, &v.EnvVars, &v.Tools, &v.CreatedAt); err != nil {
			httpx.WriteError(w, err)
			return
		}
		out = append(out, v)
	}
	httpx.JSON(w, http.StatusOK, out)
}

// addDefVersionRequest publishes a new immutable version.
type addDefVersionRequest struct {
	Config  map[string]any   `json:"config"`
	EnvVars []map[string]any `json:"envVars"`
	Tools   []map[string]any `json:"tools"`
}

func (s *Service) handleAddDefVersion(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	var req addDefVersionRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if req.Config == nil {
		req.Config = map[string]any{}
	}
	if err := validateDefVersion(req); err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	var defStatus string
	if err := s.db.QueryRow(r.Context(), `SELECT status FROM mcp_definitions WHERE id=$1`, id).Scan(&defStatus); err == pgx.ErrNoRows {
		httpx.WriteError(w, httpx.ErrNotFound("定义不存在"))
		return
	} else if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if defStatus == "archived" {
		httpx.WriteError(w, httpx.ErrConflict("已归档定义不能发布新版本"))
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	if _, err := tx.Exec(r.Context(), `SELECT id FROM mcp_definitions WHERE id=$1 FOR UPDATE`, id); err != nil {
		httpx.WriteError(w, err)
		return
	}
	var nextVer int
	if err := tx.QueryRow(r.Context(), `SELECT COALESCE(MAX(version),0)+1 FROM mcp_definition_versions WHERE definition_id=$1`, id).Scan(&nextVer); err != nil {
		httpx.WriteError(w, err)
		return
	}
	var verID int64
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO mcp_definition_versions (definition_id, version, config, env_vars, tools)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		id, nextVer, req.Config, req.EnvVars, req.Tools).Scan(&verID); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE mcp_definitions SET status='published', current_version_id=$1, updated_at=now() WHERE id=$2`, verID, id); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteError(w, err)
		return
	}
	d, aerr := s.getDefinition(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusCreated, d)
}

var errInvalidDefConfig = errors.New("config 必须包含 command 或 http url")

func validateDefVersion(req addDefVersionRequest) error {
	if cfg, ok := req.Config["command"].(string); ok && cfg != "" {
		return nil // stdio with command
	}
	if url, ok := req.Config["url"].(string); ok && strings.HasPrefix(url, "http") {
		return nil // http transport
	}
	return errInvalidDefConfig
}

func (s *Service) handleArchiveDefinition(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	tag, err := s.db.Exec(r.Context(), `UPDATE mcp_definitions SET status='archived', updated_at=now() WHERE id=$1 AND status <> 'archived'`, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteError(w, httpx.ErrNotFound("定义不存在或已归档"))
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- profiles ----

type profileDTO struct {
	ID          int64            `json:"id"`
	Name        string           `json:"name"`
	Slug        string           `json:"slug"`
	Description string           `json:"description"`
	Scope       string           `json:"scope"`
	ProjectID   *int64           `json:"projectId,omitempty"`
	Status      string           `json:"status"`
	Items       []profileItemDTO `json:"items,omitempty"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
}

type profileItemDTO struct {
	ID                  int64            `json:"id"`
	DefinitionID        int64            `json:"definitionId"`
	DefinitionSlug      string           `json:"definitionSlug"`
	DefinitionName      string           `json:"definitionName"`
	DefinitionVersionID int64            `json:"definitionVersionId"`
	Version             int              `json:"version"`
	Transport           string           `json:"transport"`
	EnabledTools        []string         `json:"enabledTools"`
	DisabledTools       []string         `json:"disabledTools"`
	Position            int              `json:"position"`
	Config              map[string]any   `json:"config"`
	EnvVars             []map[string]any `json:"envVars"`
	Tools               []map[string]any `json:"tools"`
}

func (s *Service) handleListProfiles(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r)
	q := r.URL.Query()
	where := `WHERE true`
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if kw := q.Get("keyword"); kw != "" {
		where += ` AND (name ILIKE ` + arg("%"+kw+"%") + ` OR slug ILIKE ` + arg("%"+kw+"%") + `)`
	}
	if scope := q.Get("scope"); scope != "" {
		where += ` AND scope = ` + arg(scope)
	}
	if status := q.Get("status"); status != "" {
		where += ` AND status = ` + arg(status)
	} else {
		where += ` AND status <> 'archived'`
	}
	var total int
	if err := s.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM mcp_profiles `+where, args...).Scan(&total); err != nil {
		httpx.WriteError(w, err)
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, name, slug, description, scope, project_id, status, created_at, updated_at
		FROM mcp_profiles `+where+` ORDER BY updated_at DESC LIMIT `+arg(p.PageSize)+` OFFSET `+arg(p.Offset), args...)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer rows.Close()
	items := []profileDTO{}
	for rows.Next() {
		var d profileDTO
		if err := rows.Scan(&d.ID, &d.Name, &d.Slug, &d.Description, &d.Scope, &d.ProjectID, &d.Status, &d.CreatedAt, &d.UpdatedAt); err != nil {
			httpx.WriteError(w, err)
			return
		}
		items = append(items, d)
	}
	httpx.JSON(w, http.StatusOK, httpx.PageOf(items, total, p))
}

func (s *Service) handleCreateProfile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
		Scope       string `json:"scope"`
		ProjectID   *int64 `json:"projectId"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if in.Name == "" || in.Slug == "" {
		httpx.WriteError(w, httpx.ErrUnprocessable("名称和 slug 不能为空"))
		return
	}
	if !slug.Valid(in.Slug) {
		httpx.WriteError(w, httpx.ErrUnprocessable("slug 只能包含小写字母、数字、- 和 _"))
		return
	}
	if in.Scope == "" {
		in.Scope = "global"
	}
	if in.Scope != "global" && in.Scope != "project" {
		httpx.WriteError(w, httpx.ErrUnprocessable("scope 必须是 global 或 project"))
		return
	}
	var id int64
	err := s.db.QueryRow(r.Context(), `
		INSERT INTO mcp_profiles (name, slug, description, scope, project_id)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		in.Name, in.Slug, in.Description, in.Scope, in.ProjectID).Scan(&id)
	if db.IsUniqueViolation(err) {
		httpx.WriteError(w, httpx.ErrConflict("slug 已存在"))
		return
	}
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	d, aerr := s.getProfile(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusCreated, d)
}

func (s *Service) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	d, aerr := s.getProfile(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (s *Service) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	var in struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
		Scope       string `json:"scope"`
		ProjectID   *int64 `json:"projectId"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if in.Scope == "" {
		in.Scope = "global"
	}
	_, err = s.db.Exec(r.Context(), `
		UPDATE mcp_profiles SET name=$1, slug=$2, description=$3, scope=$4, project_id=$5, updated_at=now()
		WHERE id=$6`, in.Name, in.Slug, in.Description, in.Scope, in.ProjectID, id)
	if db.IsUniqueViolation(err) {
		httpx.WriteError(w, httpx.ErrConflict("slug 已存在"))
		return
	}
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	d, aerr := s.getProfile(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (s *Service) handleListProfileItems(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	out, aerr := s.listProfileItems(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Service) handleAddProfileItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	var req struct {
		DefinitionID        int64    `json:"definitionId"`
		DefinitionVersionID int64    `json:"definitionVersionId"`
		EnabledTools        []string `json:"enabledTools"`
		DisabledTools       []string `json:"disabledTools"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if req.DefinitionID <= 0 || req.DefinitionVersionID <= 0 {
		httpx.WriteError(w, httpx.ErrUnprocessable("definitionId 和 definitionVersionId 不能为空"))
		return
	}
	var exists bool
	if err := s.db.QueryRow(r.Context(), `
		SELECT EXISTS(SELECT 1 FROM mcp_definition_versions WHERE id=$1 AND definition_id=$2)`,
		req.DefinitionVersionID, req.DefinitionID).Scan(&exists); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if !exists {
		httpx.WriteError(w, httpx.ErrUnprocessable("MCP 版本不存在"))
		return
	}
	// Definition must be published and belong to the same project scope as the profile.
	var defStatus string
	var defProject sql.NullInt64
	if err := s.db.QueryRow(r.Context(), `SELECT status, project_id FROM mcp_definitions WHERE id=$1`, req.DefinitionID).Scan(&defStatus, &defProject); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if defStatus == "archived" {
		httpx.WriteError(w, httpx.ErrUnprocessable("不能添加已归档的 MCP 定义"))
		return
	}
	var profScope string
	var profProject sql.NullInt64
	if err := s.db.QueryRow(r.Context(), `SELECT scope, project_id FROM mcp_profiles WHERE id=$1`, id).Scan(&profScope, &profProject); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if !sameNullable(profProject, defProject) {
		httpx.WriteError(w, httpx.ErrUnprocessable("Profile 与 MCP 定义的项目范围不一致"))
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	var pos int
	if err := tx.QueryRow(r.Context(), `SELECT COALESCE(MAX(position),0)+1 FROM mcp_profile_items WHERE profile_id=$1`, id).Scan(&pos); err != nil {
		httpx.WriteError(w, err)
		return
	}
	var itemID int64
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO mcp_profile_items (profile_id, definition_id, definition_version_id, enabled_tools, disabled_tools, position)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		id, req.DefinitionID, req.DefinitionVersionID, nonNil(req.EnabledTools), nonNil(req.DisabledTools), pos).Scan(&itemID); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteError(w, err)
		return
	}
	out, aerr := s.listProfileItems(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"itemId": itemID, "items": out})
}

func (s *Service) handleRemoveProfileItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	itemID, err := strconv.ParseInt(r.PathValue("itemId"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 itemId"))
		return
	}
	tag, err := s.db.Exec(r.Context(), `DELETE FROM mcp_profile_items WHERE id=$1 AND profile_id=$2`, itemID, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteError(w, httpx.ErrNotFound("条目不存在"))
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Service) handleArchiveProfile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	tag, err := s.db.Exec(r.Context(), `UPDATE mcp_profiles SET status='archived', updated_at=now() WHERE id=$1 AND status <> 'archived'`, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteError(w, httpx.ErrNotFound("Profile 不存在或已归档"))
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- helpers ----

func (s *Service) getDefinition(ctx context.Context, id int64) (definitionDTO, *httpx.Error) {
	var d definitionDTO
	var cur *int64
	err := s.db.QueryRow(ctx, `
		SELECT id, project_id, name, slug, description, category, tags, transport, status, current_version_id, created_at, updated_at
		FROM mcp_definitions WHERE id=$1`, id).
		Scan(&d.ID, &d.ProjectID, &d.Name, &d.Slug, &d.Description, &d.Category, &d.Tags, &d.Transport, &d.Status, &cur, &d.CreatedAt, &d.UpdatedAt)
	if err == pgx.ErrNoRows {
		return d, httpx.ErrNotFound("定义不存在")
	}
	if err != nil {
		return d, httpx.WrapError(err)
	}
	if cur != nil {
		if v, err := s.getDefVersion(ctx, id, *cur); err == nil {
			d.CurrentVersion = v
		}
	}
	return d, nil
}

func (s *Service) getDefVersion(ctx context.Context, defID, verID int64) (*defVersionDTO, error) {
	var v defVersionDTO
	err := s.db.QueryRow(ctx, `
		SELECT id, version, config, env_vars, tools, created_at
		FROM mcp_definition_versions WHERE id=$1 AND definition_id=$2`, verID, defID).
		Scan(&v.ID, &v.Version, &v.Config, &v.EnvVars, &v.Tools, &v.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *Service) getProfile(ctx context.Context, id int64) (profileDTO, *httpx.Error) {
	var d profileDTO
	err := s.db.QueryRow(ctx, `
		SELECT id, name, slug, description, scope, project_id, status, created_at, updated_at
		FROM mcp_profiles WHERE id=$1`, id).
		Scan(&d.ID, &d.Name, &d.Slug, &d.Description, &d.Scope, &d.ProjectID, &d.Status, &d.CreatedAt, &d.UpdatedAt)
	if err == pgx.ErrNoRows {
		return d, httpx.ErrNotFound("Profile 不存在")
	}
	if err != nil {
		return d, httpx.WrapError(err)
	}
	items, aerr := s.listProfileItems(ctx, id)
	if aerr == nil {
		d.Items = items
	}
	return d, nil
}

func (s *Service) listProfileItems(ctx context.Context, profileID int64) ([]profileItemDTO, *httpx.Error) {
	rows, err := s.db.Query(ctx, `
		SELECT pi.id, pi.definition_id, md.slug, md.name, pi.definition_version_id, mdv.version, md.transport,
		       pi.enabled_tools, pi.disabled_tools, pi.position, mdv.config, mdv.env_vars, mdv.tools
		FROM mcp_profile_items pi
		JOIN mcp_definitions md ON md.id = pi.definition_id
		JOIN mcp_definition_versions mdv ON mdv.id = pi.definition_version_id
		WHERE pi.profile_id=$1 ORDER BY pi.position`, profileID)
	if err != nil {
		return nil, httpx.WrapError(err)
	}
	defer rows.Close()
	out := []profileItemDTO{}
	for rows.Next() {
		var it profileItemDTO
		if err := rows.Scan(&it.ID, &it.DefinitionID, &it.DefinitionSlug, &it.DefinitionName, &it.DefinitionVersionID, &it.Version, &it.Transport, &it.EnabledTools, &it.DisabledTools, &it.Position, &it.Config, &it.EnvVars, &it.Tools); err != nil {
			return nil, httpx.WrapError(err)
		}
		out = append(out, it)
	}
	return out, nil
}

// installManifest is the Codex-oriented install payload.
type installManifest struct {
	Profile       profileDTO        `json:"profile"`
	ProjectSlug   *string           `json:"projectSlug,omitempty"`
	ManagedKey    string            `json:"managedKey"`
	MCPServers    []mcpServerConfig `json:"mcpServers"`
	EnabledTools  []string          `json:"enabledTools"`
	DisabledTools []string          `json:"disabledTools"`
}

type mcpServerConfig struct {
	Name          string           `json:"name"`
	Type          string           `json:"type"`
	Command       string           `json:"command,omitempty"`
	Args          []string         `json:"args,omitempty"`
	URL           string           `json:"url,omitempty"`
	Workdir       string           `json:"workdir,omitempty"`
	Env           []map[string]any `json:"env,omitempty"`
	EnabledTools  []string         `json:"enabledTools,omitempty"`
	DisabledTools []string         `json:"disabledTools,omitempty"`
}

// handleInstallManifest resolves a profile into a Codex install manifest.
func (s *Service) handleInstallManifest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	profileSlug := q.Get("profile")
	if profileSlug == "" {
		httpx.WriteError(w, httpx.ErrBadRequest("缺少 profile 参数"))
		return
	}
	ctx := r.Context()
	var pid int64
	if err := s.db.QueryRow(ctx, `SELECT id FROM mcp_profiles WHERE slug=$1 AND status <> 'archived'`, profileSlug).Scan(&pid); err == pgx.ErrNoRows {
		httpx.WriteError(w, httpx.ErrNotFound("Profile 不存在"))
		return
	} else if err != nil {
		httpx.WriteError(w, err)
		return
	}
	prof, aerr := s.getProfile(ctx, pid)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	manifest := installManifest{Profile: prof, ManagedKey: "aihub"}
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
		if url, _ := it.Config["url"].(string); url != "" {
			server.URL = url
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
		manifest.MCPServers = append(manifest.MCPServers, server)
	}
	enabledList := make([]string, 0, len(enabledSet))
	for t := range enabledSet {
		enabledList = append(enabledList, t)
	}
	sort.Strings(enabledList)
	manifest.EnabledTools = enabledList
	disabledList := make([]string, 0, len(disabledSet))
	for t := range disabledSet {
		disabledList = append(disabledList, t)
	}
	sort.Strings(disabledList)
	manifest.DisabledTools = disabledList
	if prof.ProjectID != nil {
		var slug string
		_ = s.db.QueryRow(ctx, `SELECT slug FROM projects WHERE id=$1`, *prof.ProjectID).Scan(&slug)
		manifest.ProjectSlug = &slug
	}
	httpx.JSON(w, http.StatusOK, manifest)
}

var _ = json.Marshal
