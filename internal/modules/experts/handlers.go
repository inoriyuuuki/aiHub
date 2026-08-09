package experts

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/aihub/aihub/internal/expertpack"
	"github.com/aihub/aihub/internal/platform/db"
	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

type packDTO struct {
	ID             int64           `json:"id"`
	ProjectID      *int64          `json:"projectId,omitempty"`
	Name           string          `json:"name"`
	Slug           string          `json:"slug"`
	Description    string          `json:"description"`
	Domain         string          `json:"domain"`
	Responsibility string          `json:"responsibility"`
	Usage          string          `json:"usage"`
	Status         string          `json:"status"`
	CurrentVersion *packVersionDTO `json:"currentVersion,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

type packVersionDTO struct {
	ID        int64               `json:"id"`
	Version   int                 `json:"version"`
	Manifest  expertpack.Manifest `json:"manifest"`
	SHA256    string              `json:"sha256"`
	Size      int64               `json:"size"`
	Changelog string              `json:"changelog"`
	CreatedAt time.Time           `json:"createdAt"`
	ObjectKey string              `json:"-"`
}

type memberDTO struct {
	SkillID        int64  `json:"skillId"`
	SkillSlug      string `json:"skillSlug"`
	SkillName      string `json:"skillName"`
	SkillVersionID int64  `json:"skillVersionId"`
	Version        int    `json:"version"`
	SHA256         string `json:"sha256"`
	Description    string `json:"description"`
	InstallOrder   int    `json:"installOrder"`
}

type packInput struct {
	ProjectID      *int64 `json:"projectId"`
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	Description    string `json:"description"`
	Domain         string `json:"domain"`
	Responsibility string `json:"responsibility"`
	Usage          string `json:"usage"`
}

func (s *Service) handleListPacks(w http.ResponseWriter, r *http.Request) {
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
	if status := q.Get("status"); status != "" {
		where += ` AND status = ` + arg(status)
	} else {
		where += ` AND status <> 'archived'`
	}
	var total int
	if err := s.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM expert_packs `+where, args...).Scan(&total); err != nil {
		httpx.WriteError(w, err)
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, project_id, name, slug, description, domain, responsibility, usage, status, current_version_id, created_at, updated_at
		FROM expert_packs `+where+` ORDER BY updated_at DESC LIMIT `+arg(p.PageSize)+` OFFSET `+arg(p.Offset), args...)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer rows.Close()
	items := []packDTO{}
	for rows.Next() {
		var d packDTO
		var cur *int64
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.Name, &d.Slug, &d.Description, &d.Domain, &d.Responsibility, &d.Usage, &d.Status, &cur, &d.CreatedAt, &d.UpdatedAt); err != nil {
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

func (s *Service) handleCreatePack(w http.ResponseWriter, r *http.Request) {
	var in packInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if in.Name == "" || in.Slug == "" {
		httpx.WriteError(w, httpx.ErrUnprocessable("名称和 slug 不能为空"))
		return
	}
	var id int64
	err := s.db.QueryRow(r.Context(), `
		INSERT INTO expert_packs (project_id, name, slug, description, domain, responsibility, usage)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		in.ProjectID, in.Name, in.Slug, in.Description, in.Domain, in.Responsibility, in.Usage).Scan(&id)
	if db.IsUniqueViolation(err) {
		httpx.WriteError(w, httpx.ErrConflict("slug 已存在"))
		return
	}
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	d, aerr := s.getPack(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusCreated, d)
}

func (s *Service) handleGetPack(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	d, aerr := s.getPack(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (s *Service) handleUpdatePack(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	var in packInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if in.Name == "" || in.Slug == "" {
		httpx.WriteError(w, httpx.ErrUnprocessable("名称和 slug 不能为空"))
		return
	}
	_, err = s.db.Exec(r.Context(), `
		UPDATE expert_packs SET project_id=$1, name=$2, slug=$3, description=$4, domain=$5, responsibility=$6, usage=$7, updated_at=now()
		WHERE id=$8`, in.ProjectID, in.Name, in.Slug, in.Description, in.Domain, in.Responsibility, in.Usage, id)
	if db.IsUniqueViolation(err) {
		httpx.WriteError(w, httpx.ErrConflict("slug 已存在"))
		return
	}
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	d, aerr := s.getPack(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (s *Service) handleListMembers(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT em.skill_id, sk.slug, sk.name, em.skill_version_id, sv.version, sv.sha256, sk.description, em.install_order
		FROM expert_members em
		JOIN skills sk ON sk.id = em.skill_id
		JOIN skill_versions sv ON sv.id = em.skill_version_id
		WHERE em.pack_id=$1 ORDER BY em.install_order, sk.slug`, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer rows.Close()
	out := []memberDTO{}
	for rows.Next() {
		var m memberDTO
		if err := rows.Scan(&m.SkillID, &m.SkillSlug, &m.SkillName, &m.SkillVersionID, &m.Version, &m.SHA256, &m.Description, &m.InstallOrder); err != nil {
			httpx.WriteError(w, err)
			return
		}
		out = append(out, m)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *Service) handleAddMember(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	var req struct {
		SkillID        int64 `json:"skillId"`
		SkillVersionID int64 `json:"skillVersionId"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, err)
		return
	}
	if req.SkillID <= 0 || req.SkillVersionID <= 0 {
		httpx.WriteError(w, httpx.ErrUnprocessable("skillId 和 skillVersionId 不能为空"))
		return
	}
	// Verify the version belongs to the skill and is published.
	var ver int
	if err := s.db.QueryRow(r.Context(), `
		SELECT version FROM skill_versions WHERE id=$1 AND skill_id=$2`, req.SkillVersionID, req.SkillID).Scan(&ver); err == pgx.ErrNoRows {
		httpx.WriteError(w, httpx.ErrUnprocessable("Skill 版本不存在"))
		return
	} else if err != nil {
		httpx.WriteError(w, err)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck
	var order int
	if err := tx.QueryRow(r.Context(), `SELECT COALESCE(MAX(install_order),0)+1 FROM expert_members WHERE pack_id=$1`, id).Scan(&order); err != nil {
		httpx.WriteError(w, err)
		return
	}
	_, err = tx.Exec(r.Context(), `
		INSERT INTO expert_members (pack_id, skill_id, skill_version_id, install_order)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (pack_id, skill_id) DO UPDATE SET skill_version_id=EXCLUDED.skill_version_id`,
		id, req.SkillID, req.SkillVersionID, order)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Service) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	skillID, err := strconv.ParseInt(r.PathValue("skillId"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 skillId"))
		return
	}
	tag, err := s.db.Exec(r.Context(), `DELETE FROM expert_members WHERE pack_id=$1 AND skill_id=$2`, id, skillID)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteError(w, httpx.ErrNotFound("成员不存在"))
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleBuild deterministically builds a new expert pack version.
func (s *Service) handleBuild(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	var req struct {
		Changelog string `json:"changelog"`
	}
	_ = httpx.DecodeJSON(r, &req)
	d, aerr := s.buildPack(r.Context(), id)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	_ = req
	httpx.JSON(w, http.StatusCreated, d)
}

// buildPack deterministically builds a new expert pack version.
func (s *Service) buildPack(ctx context.Context, id int64) (packDTO, *httpx.Error) {
	var pk packDTO
	var cur *int64
	if err := s.db.QueryRow(ctx, `
		SELECT id, project_id, name, slug, description, domain, responsibility, usage, status, current_version_id, created_at, updated_at
		FROM expert_packs WHERE id=$1`, id).
		Scan(&pk.ID, &pk.ProjectID, &pk.Name, &pk.Slug, &pk.Description, &pk.Domain, &pk.Responsibility, &pk.Usage, &pk.Status, &cur, &pk.CreatedAt, &pk.UpdatedAt); err == pgx.ErrNoRows {
		return pk, httpx.ErrNotFound("专家包不存在")
	} else if err != nil {
		return pk, httpx.WrapError(err)
	}
	if pk.Status == "archived" {
		return pk, httpx.ErrConflict("已归档专家包不能构建")
	}

	rows, err := s.db.Query(ctx, `
		SELECT sk.slug, sk.name, sk.description, sv.version, sv.sha256, sv.object_key
		FROM expert_members em
		JOIN skills sk ON sk.id = em.skill_id
		JOIN skill_versions sv ON sv.id = em.skill_version_id
		WHERE em.pack_id=$1 ORDER BY em.install_order, sk.slug`, id)
	if err != nil {
		return pk, httpx.WrapError(err)
	}
	defer rows.Close()
	type row struct {
		slug, name, desc, sha, objKey string
		version                       int
	}
	memberRows := []row{}
	for rows.Next() {
		var m row
		if err := rows.Scan(&m.slug, &m.name, &m.desc, &m.version, &m.sha, &m.objKey); err != nil {
			return pk, httpx.WrapError(err)
		}
		memberRows = append(memberRows, m)
	}
	if len(memberRows) == 0 {
		return pk, httpx.ErrUnprocessable("专家包至少需要一个成员 Skill")
	}

	spec := expertpack.Spec{
		PackSlug: pk.Slug, Name: pk.Name, Description: pk.Description,
		Domain: pk.Domain, Responsibility: pk.Responsibility, Usage: pk.Usage,
	}
	for _, m := range memberRows {
		files, err := s.extractSkillFiles(ctx, m.objKey)
		if err != nil {
			return pk, httpx.ErrUnprocessable("读取成员 Skill " + m.slug + " 失败: " + err.Error())
		}
		spec.Members = append(spec.Members, expertpack.Member{
			Slug: m.slug, Name: m.name, Version: m.version, SHA256: m.sha,
			Description: m.desc, Files: files,
		})
	}
	result, err := expertpack.Build(spec)
	if err != nil {
		return pk, httpx.WrapError(err)
	}
	manifest := result.Manifest
	manifestJSON, _ := expertpack.EncodeManifest(manifest)

	key := fmt.Sprintf("experts/%s/%s.zip", pk.Slug, manifest.Pack.SHA256[:16])
	if err := s.store.PutBytes(ctx, key, result.Archive, "application/zip"); err != nil {
		return pk, httpx.WrapError(err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return pk, httpx.WrapError(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var nextVer int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM expert_pack_versions WHERE pack_id=$1`, id).Scan(&nextVer); err != nil {
		return pk, httpx.WrapError(err)
	}
	manifest.Pack.Version = nextVer
	manifestJSON, _ = expertpack.EncodeManifest(manifest)
	var verID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO expert_pack_versions (pack_id, version, manifest, sha256, object_key, size, changelog)
		VALUES ($1,$2,$3,$4,$5,$6,'') RETURNING id`,
		id, nextVer, manifestJSON, manifest.Pack.SHA256, key, len(result.Archive)).Scan(&verID); err != nil {
		return pk, httpx.WrapError(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE expert_packs SET status='published', current_version_id=$1, updated_at=now() WHERE id=$2`, verID, id); err != nil {
		return pk, httpx.WrapError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return pk, httpx.WrapError(err)
	}
	return s.getPack(ctx, id)
}

func (s *Service) handleListVersions(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	rows, err := s.db.Query(r.Context(), `
		SELECT id, version, manifest, sha256, size, changelog, created_at
		FROM expert_pack_versions WHERE pack_id=$1 ORDER BY version DESC`, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	defer rows.Close()
	out := []packVersionDTO{}
	for rows.Next() {
		var v packVersionDTO
		var mj []byte
		if err := rows.Scan(&v.ID, &v.Version, &mj, &v.SHA256, &v.Size, &v.Changelog, &v.CreatedAt); err != nil {
			httpx.WriteError(w, err)
			return
		}
		_ = json.Unmarshal(mj, &v.Manifest)
		out = append(out, v)
	}
	httpx.JSON(w, http.StatusOK, out)
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
		SELECT ev.object_key, ep.slug FROM expert_pack_versions ev
		JOIN expert_packs ep ON ep.id = ev.pack_id
		WHERE ev.pack_id=$1 AND ev.version=$2`, id, v).Scan(&key, &slug); err == pgx.ErrNoRows {
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

// handleInstallManifest returns the latest manifest with a download URL.
func (s *Service) handleInstallManifest(w http.ResponseWriter, r *http.Request) {
	slug := r.URL.Query().Get("slug")
	if slug == "" {
		httpx.WriteError(w, httpx.ErrBadRequest("缺少 slug 参数"))
		return
	}
	ctx := r.Context()
	var id int64
	var cur *int64
	if err := s.db.QueryRow(ctx, `SELECT id, current_version_id FROM expert_packs WHERE slug=$1 AND status <> 'archived'`, slug).Scan(&id, &cur); err == pgx.ErrNoRows {
		httpx.WriteError(w, httpx.ErrNotFound("专家包不存在"))
		return
	} else if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if cur == nil {
		httpx.WriteError(w, httpx.ErrConflict("专家包没有已构建版本"))
		return
	}
	v, aerr := s.getVersion(ctx, id, *cur)
	if aerr != nil {
		httpx.WriteError(w, aerr)
		return
	}
	url, err := s.store.PresignGet(ctx, v.ObjectKey, 15*time.Minute)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"pack":        map[string]any{"slug": slug, "name": v.Manifest.Pack.Name, "version": v.Version, "sha256": v.SHA256},
		"manifest":    v.Manifest,
		"downloadUrl": url,
	})
}

func (s *Service) handleArchivePack(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteError(w, httpx.ErrBadRequest("无效的 id"))
		return
	}
	tag, err := s.db.Exec(r.Context(), `UPDATE expert_packs SET status='archived', updated_at=now() WHERE id=$1 AND status <> 'archived'`, id)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.WriteError(w, httpx.ErrNotFound("专家包不存在或已归档"))
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- helpers ----

func (s *Service) getPack(ctx context.Context, id int64) (packDTO, *httpx.Error) {
	var d packDTO
	var cur *int64
	err := s.db.QueryRow(ctx, `
		SELECT id, project_id, name, slug, description, domain, responsibility, usage, status, current_version_id, created_at, updated_at
		FROM expert_packs WHERE id=$1`, id).
		Scan(&d.ID, &d.ProjectID, &d.Name, &d.Slug, &d.Description, &d.Domain, &d.Responsibility, &d.Usage, &d.Status, &cur, &d.CreatedAt, &d.UpdatedAt)
	if err == pgx.ErrNoRows {
		return d, httpx.ErrNotFound("专家包不存在")
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

func (s *Service) getVersion(ctx context.Context, packID, verID int64) (*packVersionDTO, error) {
	var v packVersionDTO
	var mj []byte
	err := s.db.QueryRow(ctx, `
		SELECT id, version, manifest, sha256, object_key, size, changelog, created_at
		FROM expert_pack_versions WHERE id=$1 AND pack_id=$2`, verID, packID).
		Scan(&v.ID, &v.Version, &mj, &v.SHA256, &v.ObjectKey, &v.Size, &v.Changelog, &v.CreatedAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(mj, &v.Manifest); err != nil {
		return nil, err
	}
	return &v, nil
}

// extractSkillFiles downloads a skill zip and returns its files relative to the
// skill root directory.
func (s *Service) extractSkillFiles(ctx context.Context, objectKey string) ([]expertpack.MemberFile, error) {
	rd, _, err := s.store.Get(ctx, objectKey)
	if err != nil {
		return nil, err
	}
	defer rd.Close()
	data, err := io.ReadAll(io.LimitReader(rd, s.cfg.MaxUploadBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > s.cfg.MaxUploadBytes {
		return nil, fmt.Errorf("skill 压缩包超过大小限制")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	// Determine root dir: dir of SKILL.md.
	root := ""
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() && path.Base(f.Name) == "SKILL.md" {
			root = path.Dir(f.Name)
			if root == "." {
				root = ""
			}
			break
		}
	}
	out := []expertpack.MemberFile{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if root != "" {
			if name == root+"/SKILL.md" || strings.HasPrefix(name, root+"/") {
				name = strings.TrimPrefix(name, root+"/")
			} else {
				continue
			}
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		content, err := io.ReadAll(io.LimitReader(rc, 8<<20))
		rc.Close()
		if err != nil {
			return nil, err
		}
		out = append(out, expertpack.MemberFile{Path: name, Data: content})
	}
	return out, nil
}

var _ = sha256.Sum256
