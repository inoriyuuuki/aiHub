package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aihub/aihub/internal/platform/db"
	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/aihub/aihub/internal/skillpack"
	"github.com/jackc/pgx/v5"
)

type skillDTO struct {
	ID             int64            `json:"id"`
	ProjectID      *int64           `json:"projectId,omitempty"`
	Name           string           `json:"name"`
	Slug           string           `json:"slug"`
	Description    string           `json:"description"`
	Category       string           `json:"category"`
	Tags           []string         `json:"tags"`
	Status         string           `json:"status"`
	CurrentVersion *skillVersionDTO `json:"currentVersion,omitempty"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
}

type skillVersionDTO struct {
	ID        int64     `json:"id"`
	Version   int       `json:"version"`
	SHA256    string    `json:"sha256"`
	Size      int64     `json:"size"`
	RootDir   string    `json:"rootDir"`
	Files     []string  `json:"files"`
	Summary   string    `json:"summary"`
	Changelog string    `json:"changelog"`
	CreatedAt time.Time `json:"createdAt"`
	ObjectKey string    `json:"-"`
}

func (s *Service) handleListSkills(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r)
	q := r.URL.Query()
	where := `WHERE true`
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if kw := q.Get("keyword"); kw != "" {
		where += ` AND (name ILIKE ` + arg("%"+kw+"%") + ` OR slug ILIKE ` + arg("%"+kw+"%") + ` OR description ILIKE ` + arg("%"+kw+"%") + `)`
	}
	if cat := q.Get("category"); cat != "" {
		where += ` AND category = ` + arg(cat)
	}
	if tag := q.Get("tag"); tag != "" {
		where += ` AND ` + arg(tag) + ` = ANY(tags)`
	}
	if status := q.Get("status"); status != "" {
		where += ` AND status = ` + arg(status)
	} else {
		where += ` AND status <> 'archived'`
	}
	if pid := q.Get("projectId"); pid != "" {
		where += ` AND project_id = ` + arg(pid)
	}
	var total int
	if err := s.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM skills `+where, args...).Scan(&total); err != nil {
		httpx.WriteError(w, err)
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, project_id, name, slug, description, category, tags, status, current_version_id, created_at, updated_at
		FROM skills `+where+` ORDER BY updated_at DESC LIMIT `+arg(p.PageSize)+` OFFSET `+arg(p.Offset), args...)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer rows.Close()
	items := []skillDTO{}
	for rows.Next() {
		var d skillDTO
		var cur *int64
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.Name, &d.Slug, &d.Description, &d.Category, &d.Tags, &d.Status, &cur, &d.CreatedAt, &d.UpdatedAt); err != nil {
			httpx.WriteError(w, err)
			return
		}
		if cur != nil {
			if v, err := s.getVersion(r.Context(), d.ID, *cur); err == nil {
				d.CurrentVersion = v
			}
		}
		items = append(items, d)
	}
	httpx.JSON(w, http.StatusOK, httpx.PageOf(items, total, p))
}

// uploadSkillRequest is parsed from multipart form fields.
type uploadSkillRequest struct {
	Name        string
	Slug        string
	Description string
	Category    string
	Tags        []string
	ProjectID   *int64
	Changelog   string
	Data        []byte
	Filename    string
}

func (s *Service) handleUploadSkill(w http.ResponseWriter, r *http.Request) {
	req, aerr := parseUpload(r, s.cfg.MaxUploadBytes)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	meta, err := skillpack.Validate(req.Data, s.cfg.MaxUploadBytes)
	if err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	if req.Slug == "" {
		req.Slug = slugify(meta.Name)
	}
	if req.Name == "" {
		req.Name = meta.Name
	}
	// Store original package in MinIO.
	sha := sha256.Sum256(req.Data)
	key := fmt.Sprintf("skills/%s/%s.zip", req.Slug, hex.EncodeToString(sha[:16]))
	if err := s.store.PutBytes(r.Context(), key, req.Data, "application/zip"); err != nil {
		httpx.WriteError(w, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	var skillID int64
	err = tx.QueryRow(r.Context(), `
		INSERT INTO skills (project_id, name, slug, description, category, tags)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		req.ProjectID, req.Name, req.Slug, req.Description, req.Category, nonNil(req.Tags)).Scan(&skillID)
	if db.IsUniqueViolation(err) {
		httpx.WriteError(w, httpx.ErrConflict("Skill slug 已存在，请使用版本上传接口"))
		return
	}
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	filesJSON, _ := json.Marshal(meta.Files)
	var verID int64
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO skill_versions (skill_id, version, object_key, sha256, size, root_dir, files, changelog)
		VALUES ($1,1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		skillID, key, hex.EncodeToString(sha[:]), len(req.Data), meta.RootDir, filesJSON, req.Changelog).Scan(&verID); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE skills SET status='published', current_version_id=$1, updated_at=now() WHERE id=$2`, verID, skillID); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteError(w, err)
		return
	}
	d, aerr := s.getSkill(r.Context(), skillID)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusCreated, d)
}

func (s *Service) handleGetSkill(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	d, aerr := s.getSkill(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

// handleGetSkillBySlug resolves a skill by slug with project-first priority.
func (s *Service) handleGetSkillBySlug(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	slug := q.Get("slug")
	if slug == "" {
		httpx.WriteError(w, httpx.ErrBadRequest("缺少 slug 参数"))
		return
	}
	ctx := r.Context()
	if project := q.Get("project"); project != "" {
		var id int64
		err := s.db.QueryRow(ctx, `
			SELECT sk.id FROM skills sk JOIN projects pr ON pr.id=sk.project_id
			WHERE sk.slug=$1 AND pr.slug=$2 AND sk.status <> 'archived'`, slug, project).Scan(&id)
		if err == nil {
			d, aerr := s.getSkill(ctx, id)
			if aerr != nil {
				httpx.WriteError(w, aerr)
				return
			}
			httpx.JSON(w, http.StatusOK, d)
			return
		}
	}
	var id int64
	err := s.db.QueryRow(ctx, `SELECT id FROM skills WHERE slug=$1 AND project_id IS NULL AND status <> 'archived'`, slug).Scan(&id)
	if err == pgx.ErrNoRows {
		httpx.WriteError(w, httpx.ErrNotFound("Skill 不存在"))
		return
	}
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	d, aerr := s.getSkill(ctx, id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (s *Service) handleListVersions(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, version, object_key, sha256, size, root_dir, files, summary, changelog, created_at
		FROM skill_versions WHERE skill_id=$1 ORDER BY version DESC`, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer rows.Close()
	out := []skillVersionDTO{}
	for rows.Next() {
		var v skillVersionDTO
		var filesJSON []byte
		if err := rows.Scan(&v.ID, &v.Version, &v.ObjectKey, &v.SHA256, &v.Size, &v.RootDir, &filesJSON, &v.Summary, &v.Changelog, &v.CreatedAt); err != nil {
			httpx.WriteError(w, err)
			return
		}
		_ = json.Unmarshal(filesJSON, &v.Files)
		out = append(out, v)
	}
	httpx.JSON(w, http.StatusOK, out)
}

// handleAddVersion uploads a new version of an existing skill.
func (s *Service) handleAddVersion(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	req, aerr := parseUpload(r, s.cfg.MaxUploadBytes)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	meta, err := skillpack.Validate(req.Data, s.cfg.MaxUploadBytes)
	if err != nil {
		httpx.WriteError(w, httpx.ErrUnprocessable(err.Error()))
		return
	}
	var curSlug string
	if err := s.db.QueryRow(r.Context(), `SELECT slug FROM skills WHERE id=$1`, id).Scan(&curSlug); err == pgx.ErrNoRows {
		httpx.WriteError(w, httpx.ErrNotFound("Skill 不存在"))
		return
	} else if err != nil {
		httpx.WriteError(w, err)
		return
	}
	sha := sha256.Sum256(req.Data)
	key := fmt.Sprintf("skills/%s/%s.zip", curSlug, hex.EncodeToString(sha[:16]))
	if err := s.store.PutBytes(r.Context(), key, req.Data, "application/zip"); err != nil {
		httpx.WriteError(w, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	var nextVer int
	if err := tx.QueryRow(r.Context(), `SELECT COALESCE(MAX(version),0)+1 FROM skill_versions WHERE skill_id=$1`, id).Scan(&nextVer); err != nil {
		httpx.WriteError(w, err)
		return
	}
	filesJSON, _ := json.Marshal(meta.Files)
	var verID int64
	if err := tx.QueryRow(r.Context(), `
		INSERT INTO skill_versions (skill_id, version, object_key, sha256, size, root_dir, files, changelog)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		id, nextVer, key, hex.EncodeToString(sha[:]), len(req.Data), meta.RootDir, filesJSON, req.Changelog).Scan(&verID); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE skills SET current_version_id=$1, updated_at=now() WHERE id=$2`, verID, id); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteError(w, err)
		return
	}
	d, aerr := s.getSkill(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (s *Service) handleDownload(w http.ResponseWriter, r *http.Request) {
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
	var key, slug string
	if err := s.db.QueryRow(r.Context(), `
		SELECT sv.object_key, sk.slug FROM skill_versions sv
		JOIN skills sk ON sk.id = sv.skill_id
		WHERE sv.skill_id=$1 AND sv.version=$2`, id, v).Scan(&key, &slug); err == pgx.ErrNoRows {
		httpx.WriteError(w, httpx.ErrNotFound("版本不存在"))
		return
	} else if err != nil {
		httpx.WriteError(w, err)
		return
	}
	url, err := s.store.PresignGet(r.Context(), key, 15*time.Minute)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"url": url, "slug": slug, "version": v})
}

func (s *Service) handleArchiveSkill(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	tag, err := s.db.Exec(r.Context(), `UPDATE skills SET status='archived', updated_at=now() WHERE id=$1 AND status <> 'archived'`, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteError(w, httpx.ErrNotFound("Skill 不存在或已归档"))
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// installManifest is the resolved install payload for the CLI/Codex adapter.
type installManifest struct {
	Skill       skillDTO        `json:"skill"`
	Version     skillVersionDTO `json:"version"`
	Source      string          `json:"source"` // project | global
	Project     *string         `json:"project,omitempty"`
	DownloadURL string          `json:"downloadUrl"`
}

// handleInstallManifest resolves a skill with project-first priority.
func (s *Service) handleInstallManifest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	slug := q.Get("slug")
	project := q.Get("project")
	if slug == "" {
		httpx.WriteError(w, httpx.ErrBadRequest("缺少 slug 参数"))
		return
	}
	ctx := r.Context()
	d, ver, source, projSlug, aerr := s.resolveSkill(ctx, slug, project)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	url, err := s.store.PresignGet(ctx, ver.ObjectKey, 15*time.Minute)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	manifest := installManifest{Skill: d, Version: *ver, Source: source, DownloadURL: url}
	if projSlug != "" {
		manifest.Project = &projSlug
	}
	httpx.JSON(w, http.StatusOK, manifest)
}

// resolveSkill returns the skill DTO, current version and source ("project"/"global").
func (s *Service) resolveSkill(ctx context.Context, slug, project string) (skillDTO, *skillVersionDTO, string, string, *httpx.Error) {
	query := `
		SELECT sk.id, sk.project_id, sk.name, sk.slug, sk.description, sk.category, sk.tags, sk.status,
		       sk.current_version_id, sk.created_at, sk.updated_at, pr.slug
		FROM skills sk LEFT JOIN projects pr ON pr.id = sk.project_id
		WHERE sk.slug=$1 AND sk.status <> 'archived'`
	args := []any{slug}
	if project != "" {
		query += ` AND pr.slug = $2`
		args = append(args, project)
	}
	query += ` ORDER BY (sk.project_id IS NOT NULL) DESC LIMIT 1`

	var d skillDTO
	var cur *int64
	var projSlug *string
	err := s.db.QueryRow(ctx, query, args...).Scan(&d.ID, &d.ProjectID, &d.Name, &d.Slug, &d.Description, &d.Category, &d.Tags, &d.Status, &cur, &d.CreatedAt, &d.UpdatedAt, &projSlug)
	if err == pgx.ErrNoRows {
		return d, nil, "", "", httpx.ErrNotFound("Skill 不存在: " + slug)
	}
	if err != nil {
		return d, nil, "", "", httpx.WrapError(err)
	}
	if cur == nil {
		return d, nil, "", "", httpx.ErrConflict("Skill 没有已发布版本")
	}
	v, err := s.getVersion(ctx, d.ID, *cur)
	if err != nil {
		return d, nil, "", "", httpx.WrapError(err)
	}
	source := "global"
	ps := ""
	if d.ProjectID != nil && projSlug != nil {
		source = "project"
		ps = *projSlug
	}
	return d, v, source, ps, nil
}

func (s *Service) getSkill(ctx context.Context, id int64) (skillDTO, *httpx.Error) {
	var d skillDTO
	var cur *int64
	err := s.db.QueryRow(ctx, `
		SELECT id, project_id, name, slug, description, category, tags, status, current_version_id, created_at, updated_at
		FROM skills WHERE id=$1`, id).
		Scan(&d.ID, &d.ProjectID, &d.Name, &d.Slug, &d.Description, &d.Category, &d.Tags, &d.Status, &cur, &d.CreatedAt, &d.UpdatedAt)
	if err == pgx.ErrNoRows {
		return d, httpx.ErrNotFound("Skill 不存在")
	}
	if err != nil {
		return d, httpx.WrapError(err)
	}
	if cur != nil {
		if v, err := s.getVersion(ctx, id, *cur); err == nil {
			d.CurrentVersion = v
		}
	}
	return d, nil
}

func (s *Service) getVersion(ctx context.Context, skillID, verID int64) (*skillVersionDTO, error) {
	var v skillVersionDTO
	var filesJSON []byte
	err := s.db.QueryRow(ctx, `
		SELECT id, version, object_key, sha256, size, root_dir, files, summary, changelog, created_at
		FROM skill_versions WHERE id=$1 AND skill_id=$2`, verID, skillID).
		Scan(&v.ID, &v.Version, &v.ObjectKey, &v.SHA256, &v.Size, &v.RootDir, &filesJSON, &v.Summary, &v.Changelog, &v.CreatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(filesJSON, &v.Files)
	return &v, nil
}

// parseUpload reads multipart form fields and the zip file.
func parseUpload(r *http.Request, maxBytes int64) (*uploadSkillRequest, *httpx.Error) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return nil, httpx.ErrBadRequest("无法解析上传表单: " + err.Error())
	}
	req := &uploadSkillRequest{
		Name:        r.FormValue("name"),
		Slug:        r.FormValue("slug"),
		Description: r.FormValue("description"),
		Category:    r.FormValue("category"),
		Changelog:   r.FormValue("changelog"),
	}
	if tags := r.FormValue("tags"); tags != "" {
		_ = json.Unmarshal([]byte(tags), &req.Tags)
	}
	if pid := r.FormValue("projectId"); pid != "" {
		n, err := strconv.ParseInt(pid, 10, 64)
		if err == nil {
			req.ProjectID = &n
		}
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, httpx.ErrBadRequest("缺少 file 字段")
	}
	defer file.Close()
	if header.Size > maxBytes {
		return nil, httpx.ErrUnprocessable("文件超过大小限制")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, httpx.WrapError(err)
	}
	if int64(len(data)) > maxBytes {
		return nil, httpx.ErrUnprocessable("文件超过大小限制")
	}
	req.Data = data
	req.Filename = header.Filename
	return req, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func slugify(name string) string {
	out := []rune{}
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		case r == ' ', r == '_', r == '.':
			out = append(out, '-')
		}
	}
	return strings.Trim(string(out), "-")
}
