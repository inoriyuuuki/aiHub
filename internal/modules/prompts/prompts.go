package prompts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/sergi/go-diff/diffmatchpatch"
)

type promptDTO struct {
	ID             int64       `json:"id"`
	ProjectID      *int64      `json:"projectId,omitempty"`
	CategoryID     int64       `json:"categoryId"`
	CategoryName   string      `json:"categoryName,omitempty"`
	Slug           string      `json:"slug"`
	Title          string      `json:"title"`
	Description    string      `json:"description"`
	Tags           []string    `json:"tags"`
	Status         string      `json:"status"`
	CurrentVersion *versionDTO `json:"currentVersion,omitempty"`
	Draft          *draftDTO   `json:"draft,omitempty"`
	CreatedAt      time.Time   `json:"createdAt"`
	UpdatedAt      time.Time   `json:"updatedAt"`
}

type draftDTO struct {
	Content   map[string]any `json:"content"`
	Variables []string       `json:"variables"`
	Summary   string         `json:"summary"`
}

type versionDTO struct {
	ID        int64          `json:"id"`
	Version   int            `json:"version"`
	Content   map[string]any `json:"content"`
	Variables []string       `json:"variables"`
	Summary   string         `json:"summary"`
	SchemaID  int64          `json:"schemaId"`
	CreatedAt time.Time      `json:"createdAt"`
}

type promptInput struct {
	ProjectID   *int64         `json:"projectId"`
	CategoryID  int64          `json:"categoryId"`
	Slug        string         `json:"slug"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Tags        []string       `json:"tags"`
	Content     map[string]any `json:"content"`
	Summary     string         `json:"summary"`
}

func (s *Service) handleListPrompts(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r)
	q := r.URL.Query()
	where := `WHERE true`
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if kw := q.Get("keyword"); kw != "" {
		where += ` AND (p.title ILIKE ` + arg("%"+kw+"%") + ` OR p.slug ILIKE ` + arg("%"+kw+"%") + `)`
	}
	if cat := q.Get("category"); cat != "" {
		where += ` AND p.category_id = ` + arg(cat)
	}
	if tag := q.Get("tag"); tag != "" {
		where += ` AND ` + arg(tag) + ` = ANY(p.tags)`
	}
	if status := q.Get("status"); status != "" {
		where += ` AND p.status = ` + arg(status)
	}
	if pid := q.Get("projectId"); pid != "" {
		where += ` AND p.project_id = ` + arg(pid)
	}
	var total int
	if err := s.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM prompts p `+where, args...).Scan(&total); err != nil {
		httpx.WriteError(w, err)
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT p.id, p.project_id, p.category_id, pc.name, p.slug, p.title, p.description, p.tags, p.status,
		       p.current_version_id, p.created_at, p.updated_at
		FROM prompts p LEFT JOIN prompt_categories pc ON pc.id = p.category_id
		`+where+` ORDER BY p.updated_at DESC
		LIMIT `+arg(p.PageSize)+` OFFSET `+arg(p.Offset), args...)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer rows.Close()
	items := []promptDTO{}
	for rows.Next() {
		var d promptDTO
		var curVerID *int64
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.CategoryID, &d.CategoryName, &d.Slug, &d.Title, &d.Description, &d.Tags, &d.Status, &curVerID, &d.CreatedAt, &d.UpdatedAt); err != nil {
			httpx.WriteError(w, err)
			return
		}
		if curVerID != nil {
			if v, err := s.getVersion(r.Context(), d.ID, *curVerID); err == nil {
				d.CurrentVersion = v
			}
		}
		items = append(items, d)
	}
	httpx.JSON(w, http.StatusOK, httpx.PageOf(items, total, p))
}

func (s *Service) handleCreatePrompt(w http.ResponseWriter, r *http.Request) {
	var in promptInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := s.validatePromptInput(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	id, aerr := s.createPrompt(r.Context(), in)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	d, aerr := s.getPrompt(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusCreated, d)
}

func (s *Service) handleGetPrompt(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	d, aerr := s.getPrompt(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

// handleGetPromptBySlug resolves a prompt by slug with project-first priority:
// if both a project and global resource share the slug, the project one wins.
func (s *Service) handleGetPromptBySlug(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	slug := q.Get("slug")
	project := q.Get("project")
	if slug == "" {
		httpx.WriteError(w, httpx.ErrBadRequest("缺少 slug 参数"))
		return
	}
	ctx := r.Context()
	var id int64
	if project != "" {
		err := s.db.QueryRow(ctx, `
			SELECT p.id FROM prompts p
			JOIN projects pr ON pr.id = p.project_id
			WHERE p.slug=$1 AND pr.slug=$2 AND p.status <> 'archived'`, slug, project).Scan(&id)
		if err == nil {
			d, aerr := s.getPrompt(ctx, id)
			if aerr != nil {
				httpx.WriteError(w, aerr)
				return
			}
			httpx.JSON(w, http.StatusOK, d)
			return
		}
	}
	err := s.db.QueryRow(ctx, `SELECT id FROM prompts WHERE slug=$1 AND project_id IS NULL AND status <> 'archived'`, slug).Scan(&id)
	if err == pgx.ErrNoRows {
		httpx.WriteError(w, httpx.ErrNotFound("提示词不存在"))
		return
	}
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	d, aerr := s.getPrompt(ctx, id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (s *Service) handleUpdatePrompt(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	var in promptInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := s.validatePromptInput(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	d, aerr := s.updatePrompt(r.Context(), id, in)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

// handlePublish creates the next immutable version from the draft.
func (s *Service) handlePublish(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	var req struct {
		Summary string `json:"summary"`
	}
	_ = httpx.DecodeJSON(r, &req)
	d, aerr := s.publishPrompt(r.Context(), id, req.Summary)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusCreated, d)
}

func (s *Service) handleListVersions(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, version, content, variables, summary, schema_id, created_at
		FROM prompt_versions WHERE prompt_id=$1 AND version>0 ORDER BY version DESC`, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer rows.Close()
	out := []versionDTO{}
	for rows.Next() {
		var v versionDTO
		if err := rows.Scan(&v.ID, &v.Version, &v.Content, &v.Variables, &v.Summary, &v.SchemaID, &v.CreatedAt); err != nil {
			httpx.WriteError(w, err)
			return
		}
		out = append(out, v)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Service) handleGetVersion(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	v, err := strconv.Atoi(r.PathValue("v"))
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的版本号"))
		return
	}
	ver, aerr := s.getVersionByNumber(r.Context(), id, v)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusOK, ver)
}

// handleDiff returns a textual diff between two versions (or draft vs version).
func (s *Service) handleDiff(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	v, err := strconv.Atoi(r.PathValue("v"))
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的版本号"))
		return
	}
	base := r.URL.Query().Get("base")
	target, aerr := s.getVersionByNumber(r.Context(), id, v)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	var baseContent map[string]any
	if base == "draft" {
		baseContent, err = s.getDraftContent(r.Context(), id)
		if err != nil {
			httpx.WriteError(w, httpx.ErrNotFound("草稿不存在"))
			return
		}
	} else {
		b, berr := strconv.Atoi(base)
		if berr != nil {
			httpx.WriteError(w, httpx.ErrBadRequest("base 必须是版本号或 draft"))
			return
		}
		bv, aerr := s.getVersionByNumber(r.Context(), id, b)
		if aerr != nil {
			httpx.WriteError(w, aerr)
			return
		}
		baseContent = bv.Content
	}
	diff := diffJSON(baseContent, target.Content)
	httpx.JSON(w, http.StatusOK, map[string]any{"diff": diff, "base": base, "target": v})
}

// handleRollback creates a new version from a past version's content.
func (s *Service) handleRollback(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	var req struct {
		Version int    `json:"version"`
		Summary string `json:"summary"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	ctx := r.Context()
	var status string
	if err := s.db.QueryRow(ctx, `SELECT status FROM prompts WHERE id=$1`, id).Scan(&status); err == pgx.ErrNoRows {
		httpx.WriteError(w, httpx.ErrNotFound("提示词不存在"))
		return
	} else if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if status == "archived" {
		httpx.WriteError(w, httpx.ErrConflict("已归档提示词不能回滚"))
		return
	}
	src, aerr := s.getVersionByNumber(ctx, id, req.Version)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var nextVer int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM prompt_versions WHERE prompt_id=$1 AND version>0`, id).Scan(&nextVer); err != nil {
		httpx.WriteError(w, err)
		return
	}
	var verID int64
	summary := req.Summary
	if summary == "" {
		summary = fmt.Sprintf("回滚到版本 %d", req.Version)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO prompt_versions (prompt_id, version, content, variables, schema_id, summary)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		id, nextVer, src.Content, src.Variables, src.SchemaID, summary).Scan(&verID); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if _, err := tx.Exec(ctx, `UPDATE prompts SET current_version_id=$1, updated_at=now() WHERE id=$2`, verID, id); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		httpx.WriteError(w, err)
		return
	}
	d, aerr := s.getPrompt(ctx, id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

// handleRender renders the published (or specified) version's content with values.
func (s *Service) handleRender(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	var req struct {
		Values  map[string]any `json:"values"`
		Version *int           `json:"version,omitempty"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	ctx := r.Context()
	var content map[string]any
	if req.Version != nil {
		v, aerr := s.getVersionByNumber(ctx, id, *req.Version)
		if aerr != nil {
			httpx.WriteError(w, aerr)
			return
		}
		content = v.Content
	} else {
		content, err = s.getDraftContent(ctx, id)
		if err != nil {
			// Fall back to current version if there is no draft.
			var curID *int64
			_ = s.db.QueryRow(ctx, `SELECT current_version_id FROM prompts WHERE id=$1`, id).Scan(&curID)
			if curID == nil {
				httpx.WriteError(w, httpx.ErrNotFound("没有可渲染的内容"))
				return
			}
			v, aerr := s.getVersion(ctx, id, *curID)
			if aerr != nil {
				httpx.WriteError(w, aerr)
				return
			}
			content = v.Content
		}
	}
	rendered := RenderContent(content, req.Values)
	httpx.JSON(w, http.StatusOK, rendered)
}

func (s *Service) handleArchivePrompt(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	tag, err := s.db.Exec(r.Context(), `UPDATE prompts SET status='archived', updated_at=now() WHERE id=$1 AND status <> 'archived'`, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteError(w, httpx.ErrNotFound("提示词不存在或已归档"))
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- internal helpers ----

func (s *Service) validatePromptInput(r *http.Request, in *promptInput) *httpx.Error {
	if in.Slug == "" || in.Title == "" {
		return httpx.ErrUnprocessable("slug 和 title 不能为空")
	}
	if in.CategoryID <= 0 {
		return httpx.ErrUnprocessable("categoryId 不能为空")
	}
	return nil
}

func (s *Service) currentSchemaID(ctx context.Context, categoryID int64) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `
		SELECT id FROM prompt_schemas WHERE category_id=$1 ORDER BY version DESC LIMIT 1`, categoryID).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, fmt.Errorf("分类没有表单 Schema")
	}
	return id, err
}

// validateAndExtract validates draft content and returns used variables.
func (s *Service) validateAndExtract(in promptInput, schemaID int64) ([]string, *httpx.Error) {
	if in.Content == nil {
		return nil, nil
	}
	var schema map[string]any
	if err := s.db.QueryRow(context.Background(), `SELECT schema FROM prompt_schemas WHERE id=$1`, schemaID).Scan(&schema); err != nil {
		return nil, httpx.WrapError(err)
	}
	if err := ValidateContent(schema, in.Content); err != nil {
		return nil, httpx.ErrUnprocessable("内容校验失败: " + err.Error())
	}
	used, err := ValidateVariables(schema, in.Content)
	if err != nil {
		return nil, httpx.ErrUnprocessable(err.Error())
	}
	return used, nil
}

func (s *Service) getPrompt(ctx context.Context, id int64) (promptDTO, *httpx.Error) {
	var d promptDTO
	var curVerID *int64
	err := s.db.QueryRow(ctx, `
		SELECT p.id, p.project_id, p.category_id, pc.name, p.slug, p.title, p.description, p.tags, p.status,
		       p.current_version_id, p.created_at, p.updated_at
		FROM prompts p LEFT JOIN prompt_categories pc ON pc.id = p.category_id
		WHERE p.id=$1`, id).
		Scan(&d.ID, &d.ProjectID, &d.CategoryID, &d.CategoryName, &d.Slug, &d.Title, &d.Description, &d.Tags, &d.Status, &curVerID, &d.CreatedAt, &d.UpdatedAt)
	if err == pgx.ErrNoRows {
		return d, httpx.ErrNotFound("提示词不存在")
	}
	if err != nil {
		return d, httpx.WrapError(err)
	}
	if curVerID != nil {
		if v, err := s.getVersion(ctx, id, *curVerID); err == nil {
			d.CurrentVersion = v
		}
	}
	if content, err := s.getDraftContent(ctx, id); err == nil {
		var vars []string
		_ = s.db.QueryRow(ctx, `SELECT variables FROM prompt_versions WHERE prompt_id=$1 AND version=0`, id).Scan(&vars)
		d.Draft = &draftDTO{Content: content, Variables: vars}
	}
	return d, nil
}

func (s *Service) getVersion(ctx context.Context, promptID, verID int64) (*versionDTO, error) {
	var v versionDTO
	err := s.db.QueryRow(ctx, `
		SELECT id, version, content, variables, summary, schema_id, created_at
		FROM prompt_versions WHERE id=$1 AND prompt_id=$2 AND version>0`, verID, promptID).
		Scan(&v.ID, &v.Version, &v.Content, &v.Variables, &v.Summary, &v.SchemaID, &v.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *Service) getVersionByNumber(ctx context.Context, promptID int64, v int) (*versionDTO, *httpx.Error) {
	var out versionDTO
	err := s.db.QueryRow(ctx, `
		SELECT id, version, content, variables, summary, schema_id, created_at
		FROM prompt_versions WHERE prompt_id=$1 AND version=$2 AND version>0`, promptID, v).
		Scan(&out.ID, &out.Version, &out.Content, &out.Variables, &out.Summary, &out.SchemaID, &out.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, httpx.ErrNotFound("版本不存在")
	}
	if err != nil {
		return nil, httpx.WrapError(err)
	}
	return &out, nil
}

func (s *Service) getDraftContent(ctx context.Context, promptID int64) (map[string]any, error) {
	var c map[string]any
	err := s.db.QueryRow(ctx, `SELECT content FROM prompt_versions WHERE prompt_id=$1 AND version=0`, promptID).Scan(&c)
	return c, err
}

// diffJSON renders a unified-style diff of two JSON documents as text.
func diffJSON(a, b map[string]any) string {
	ab, _ := json.MarshalIndent(a, "", "  ")
	bb, _ := json.MarshalIndent(b, "", "  ")
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(string(ab), string(bb), false)
	return dmp.DiffPrettyText(diffs)
}
