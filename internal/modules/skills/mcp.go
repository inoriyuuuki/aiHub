package skills

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/aihub/aihub/internal/mcpx"
	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/aihub/aihub/internal/skillpack"
	"github.com/jackc/pgx/v5"
)

func (s *Service) mcpTools() []mcpx.ToolDef {
	return []mcpx.ToolDef{
		{
			Name:        "skills.read",
			Description: "搜索 Skill 或获取安装清单",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword":  map[string]any{"type": "string"},
					"slug":     map[string]any{"type": "string"},
					"project":  map[string]any{"type": "string"},
					"manifest": map[string]any{"type": "boolean"},
					"page":     map[string]any{"type": "integer"},
					"pageSize": map[string]any{"type": "integer"},
				},
			},
			Group:   "skills",
			Handler: s.mcpReadSkills,
		},
		{
			Name:        "skills.write",
			Description: "发布 Skill 新版本",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug":      map[string]any{"type": "string"},
					"changelog": map[string]any{"type": "string"},
					"zipBase64": map[string]any{"type": "string"},
				},
				"required": []any{"slug", "zipBase64"},
			},
			Write:   true,
			Group:   "skills",
			Handler: s.mcpWriteSkill,
		},
		{
			Name:        "skills.delete",
			Description: "归档 Skill",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"slug": map[string]any{"type": "string"},
				},
				"required": []any{"slug"},
			},
			Write:   false,
			Delete:  true,
			Group:   "skills",
			Handler: s.mcpDeleteSkill,
		},
	}
}

func (s *Service) mcpReadSkills(ctx context.Context, args map[string]any) (any, error) {
	if manifest, _ := args["manifest"].(bool); manifest {
		slug, _ := args["slug"].(string)
		project, _ := args["project"].(string)
		if slug == "" {
			return nil, httpx.ErrBadRequest("获取安装清单需要 slug")
		}
		d, ver, source, projSlug, aerr := s.resolveSkill(ctx, slug, project)
		if aerr != nil {
			return nil, aerr
		}
		return map[string]any{
			"skill": d, "version": ver, "source": source,
			"project": projSlug, "downloadUrl": "GET /api/v1/skills/" + strconv.FormatInt(d.ID, 10) + "/versions/" + strconv.Itoa(ver.Version) + "/download",
		}, nil
	}
	if slug, _ := args["slug"].(string); slug != "" {
		project, _ := args["project"].(string)
		d, ver, source, projSlug, aerr := s.resolveSkill(ctx, slug, project)
		if aerr != nil {
			return nil, aerr
		}
		return map[string]any{"skill": d, "version": ver, "source": source, "project": projSlug}, nil
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
		where += ` AND (name ILIKE ` + arg("%"+kw+"%") + ` OR slug ILIKE ` + arg("%"+kw+"%") + ` OR description ILIKE ` + arg("%"+kw+"%") + `)`
	}
	var total int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM skills `+where, qargs...).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, project_id, name, slug, description, category, tags, status, current_version_id, created_at, updated_at
		FROM skills `+where+` ORDER BY updated_at DESC LIMIT `+arg(size)+` OFFSET `+arg((page-1)*size), qargs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []skillDTO{}
	for rows.Next() {
		var d skillDTO
		var cur *int64
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.Name, &d.Slug, &d.Description, &d.Category, &d.Tags, &d.Status, &cur, &d.CreatedAt, &d.UpdatedAt); err != nil {
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

func (s *Service) mcpWriteSkill(ctx context.Context, args map[string]any) (any, error) {
	slug, _ := args["slug"].(string)
	changelog, _ := args["changelog"].(string)
	encoded, _ := args["zipBase64"].(string)
	if slug == "" || encoded == "" {
		return nil, httpx.ErrUnprocessable("缺少 slug 或 zipBase64")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, httpx.ErrUnprocessable("zipBase64 不是合法的 base64")
	}
	if int64(len(data)) > s.cfg.MaxUploadBytes {
		return nil, httpx.ErrUnprocessable("压缩包超过大小限制")
	}
	meta, err := skillpack.Validate(data, s.cfg.MaxUploadBytes)
	if err != nil {
		return nil, httpx.ErrUnprocessable(err.Error())
	}
	var id int64
	err = s.db.QueryRow(ctx, `SELECT id FROM skills WHERE slug=$1 AND status <> 'archived'`, slug).Scan(&id)
	if err == pgx.ErrNoRows {
		return nil, httpx.ErrNotFound("Skill 不存在: " + slug)
	}
	if err != nil {
		return nil, err
	}
	sha := sha256.Sum256(data)
	key := fmt.Sprintf("skills/%s/%s.zip", slug, hex.EncodeToString(sha[:16]))
	if err := s.store.PutBytes(ctx, key, data, "application/zip"); err != nil {
		return nil, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var nextVer int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM skill_versions WHERE skill_id=$1`, id).Scan(&nextVer); err != nil {
		return nil, err
	}
	filesJSON, _ := json.Marshal(meta.Files)
	var verID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO skill_versions (skill_id, version, object_key, sha256, size, root_dir, files, changelog)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		id, nextVer, key, hex.EncodeToString(sha[:]), len(data), meta.RootDir, filesJSON, changelog).Scan(&verID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE skills SET current_version_id=$1, updated_at=now() WHERE id=$2`, verID, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	d, aerr := s.getSkill(ctx, id)
	if aerr != nil {
		return nil, aerr
	}
	return d, nil
}

func (s *Service) mcpDeleteSkill(ctx context.Context, args map[string]any) (any, error) {
	slug, _ := args["slug"].(string)
	if slug == "" {
		return nil, httpx.ErrUnprocessable("缺少 slug")
	}
	tag, err := s.db.Exec(ctx, `UPDATE skills SET status='archived', updated_at=now() WHERE slug=$1 AND status <> 'archived'`, slug)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, httpx.ErrNotFound("Skill 不存在或已归档")
	}
	return map[string]bool{"ok": true}, nil
}
