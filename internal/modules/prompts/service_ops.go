package prompts

import (
	"context"

	"github.com/aihub/aihub/internal/platform/db"
	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

// createPrompt inserts a new prompt (optionally with draft content).
func (s *Service) createPrompt(ctx context.Context, in promptInput) (int64, *httpx.Error) {
	var id int64
	err := s.db.QueryRow(ctx, `
		INSERT INTO prompts (project_id, category_id, slug, title, description, tags)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		in.ProjectID, in.CategoryID, in.Slug, in.Title, in.Description, strSlice(in.Tags)).Scan(&id)
	if db.IsUniqueViolation(err) {
		return 0, httpx.ErrConflict("提示词 slug 已存在")
	}
	if err != nil {
		if s.log != nil {
			s.log.Error("createPrompt insert failed", "error", err)
		}
		return 0, httpx.WrapError(err)
	}
	if in.Content != nil {
		if err := s.saveDraft(ctx, id, in); err != nil {
			if s.log != nil {
				s.log.Error("createPrompt saveDraft failed", "error", err)
			}
			return 0, httpx.WrapError(err)
		}
	}
	return id, nil
}

// updatePrompt updates a draft prompt and its content.
func (s *Service) updatePrompt(ctx context.Context, id int64, in promptInput) (promptDTO, *httpx.Error) {
	var status string
	if err := s.db.QueryRow(ctx, `SELECT status FROM prompts WHERE id=$1`, id).Scan(&status); err == pgx.ErrNoRows {
		return promptDTO{}, httpx.ErrNotFound("提示词不存在")
	} else if err != nil {
		return promptDTO{}, httpx.WrapError(err)
	}
	if status == "archived" {
		return promptDTO{}, httpx.ErrConflict("已归档提示词不能编辑")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return promptDTO{}, httpx.WrapError(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	_, err = tx.Exec(ctx, `
		UPDATE prompts SET project_id=$1, category_id=$2, slug=$3, title=$4, description=$5, tags=$6, updated_at=now()
		WHERE id=$7`, in.ProjectID, in.CategoryID, in.Slug, in.Title, in.Description, strSlice(in.Tags), id)
	if db.IsUniqueViolation(err) {
		return promptDTO{}, httpx.ErrConflict("提示词 slug 已存在")
	}
	if err != nil {
		return promptDTO{}, httpx.WrapError(err)
	}
	if err := s.saveDraftTx(ctx, tx, id, in); err != nil {
		return promptDTO{}, httpx.WrapError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return promptDTO{}, httpx.WrapError(err)
	}
	return s.getPrompt(ctx, id)
}

// publishPrompt publishes the draft as the next immutable version.
func (s *Service) publishPrompt(ctx context.Context, id int64, summary string) (promptDTO, *httpx.Error) {
	var status string
	var categoryID int64
	if err := s.db.QueryRow(ctx, `SELECT status, category_id FROM prompts WHERE id=$1`, id).Scan(&status, &categoryID); err == pgx.ErrNoRows {
		return promptDTO{}, httpx.ErrNotFound("提示词不存在")
	} else if err != nil {
		return promptDTO{}, httpx.WrapError(err)
	}
	if status == "archived" {
		return promptDTO{}, httpx.ErrConflict("已归档提示词不能发布")
	}
	var draftContent map[string]any
	var draftSchemaID int64
	err := s.db.QueryRow(ctx, `
		SELECT content, schema_id FROM prompt_versions WHERE prompt_id=$1 AND version=0`, id).
		Scan(&draftContent, &draftSchemaID)
	if err == pgx.ErrNoRows {
		return promptDTO{}, httpx.ErrUnprocessable("草稿内容为空，请先保存内容")
	}
	if err != nil {
		return promptDTO{}, httpx.WrapError(err)
	}
	var schema map[string]any
	if err := s.db.QueryRow(ctx, `SELECT schema FROM prompt_schemas WHERE id=$1`, draftSchemaID).Scan(&schema); err != nil {
		return promptDTO{}, httpx.WrapError(err)
	}
	if err := ValidateContent(schema, draftContent); err != nil {
		return promptDTO{}, httpx.ErrUnprocessable("内容校验失败: " + err.Error())
	}
	used, err := ValidateVariables(schema, draftContent)
	if err != nil {
		return promptDTO{}, httpx.ErrUnprocessable(err.Error())
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return promptDTO{}, httpx.WrapError(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var nextVer int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM prompt_versions WHERE prompt_id=$1 AND version>0`, id).Scan(&nextVer); err != nil {
		return promptDTO{}, httpx.WrapError(err)
	}
	var verID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO prompt_versions (prompt_id, version, content, variables, schema_id, summary)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		id, nextVer, draftContent, used, draftSchemaID, summary).Scan(&verID); err != nil {
		return promptDTO{}, httpx.WrapError(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE prompts SET status='published', current_version_id=$1, updated_at=now() WHERE id=$2`, verID, id); err != nil {
		return promptDTO{}, httpx.WrapError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return promptDTO{}, httpx.WrapError(err)
	}
	return s.getPrompt(ctx, id)
}

// saveDraft validates and upserts the draft version (version 0) in its own tx.
func (s *Service) saveDraft(ctx context.Context, id int64, in promptInput) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := s.saveDraftTx(ctx, tx, id, in); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// saveDraftTx validates and upserts the draft version within an existing tx.
func (s *Service) saveDraftTx(ctx context.Context, tx pgx.Tx, id int64, in promptInput) error {
	if in.Content == nil {
		return nil
	}
	schemaID, err := s.currentSchemaID(ctx, in.CategoryID)
	if err != nil {
		return err
	}
	used, verr := s.validateAndExtract(in, schemaID)
	if verr != nil {
		return verr
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO prompt_versions (prompt_id, version, content, variables, schema_id, summary)
		VALUES ($1,0,$2,$3,$4,$5)
		ON CONFLICT (prompt_id, version) DO UPDATE SET
			content=EXCLUDED.content, variables=EXCLUDED.variables, schema_id=EXCLUDED.schema_id, summary=EXCLUDED.summary`,
		id, in.Content, used, schemaID, in.Summary)
	return err
}

// strSlice returns an empty slice instead of nil so NOT NULL text[] columns
// are not violated.
func strSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
