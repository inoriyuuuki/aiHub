package prompts

import (
	"context"

	"github.com/aihub/aihub/internal/platform/db"
	"github.com/aihub/aihub/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

// querier abstracts the pool and transactions so validation helpers can run
// against either.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// createPrompt inserts a new prompt (optionally with draft content) in a
// single transaction so a failed draft save cannot orphan the prompt row.
func (s *Service) createPrompt(ctx context.Context, in promptInput) (int64, *httpx.Error) {
	schemaID, used, verr := s.prepareDraft(ctx, s.db, in)
	if verr != nil {
		return 0, verr
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, httpx.WrapError(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var id int64
	err = tx.QueryRow(ctx, `
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
		if _, err := tx.Exec(ctx, `
			INSERT INTO prompt_versions (prompt_id, version, content, variables, schema_id, summary)
			VALUES ($1,0,$2,$3,$4,$5)`, id, in.Content, used, schemaID, in.Summary); err != nil {
			return 0, httpx.WrapError(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, httpx.WrapError(err)
	}
	return id, nil
}

// updatePrompt updates a draft prompt and its content in one transaction.
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
	schemaID, used, verr := s.prepareDraft(ctx, s.db, in)
	if verr != nil {
		return promptDTO{}, verr
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
	if in.Content != nil {
		if _, err := tx.Exec(ctx, `
			INSERT INTO prompt_versions (prompt_id, version, content, variables, schema_id, summary)
			VALUES ($1,0,$2,$3,$4,$5)
			ON CONFLICT (prompt_id, version) DO UPDATE SET
				content=EXCLUDED.content, variables=EXCLUDED.variables, schema_id=EXCLUDED.schema_id, summary=EXCLUDED.summary`,
			id, in.Content, used, schemaID, in.Summary); err != nil {
			return promptDTO{}, httpx.WrapError(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return promptDTO{}, httpx.WrapError(err)
	}
	return s.getPrompt(ctx, id)
}

// prepareDraft validates the category and content against the current schema
// and returns the schema id + used variables. It must run before any write so
// failed validation never leaves an orphan row.
func (s *Service) prepareDraft(ctx context.Context, q querier, in promptInput) (int64, []string, *httpx.Error) {
	if in.Content == nil {
		return 0, nil, nil
	}
	// Category must exist, not be archived, and belong to the same project.
	var catProject *int64
	var archived bool
	err := q.QueryRow(ctx, `SELECT project_id, archived FROM prompt_categories WHERE id=$1`, in.CategoryID).Scan(&catProject, &archived)
	if err == pgx.ErrNoRows {
		return 0, nil, httpx.ErrUnprocessable("分类不存在")
	}
	if err != nil {
		return 0, nil, httpx.WrapError(err)
	}
	if archived {
		return 0, nil, httpx.ErrUnprocessable("分类已归档")
	}
	// A global category may be used by any prompt; a project category must
	// belong to the same project as the prompt.
	if catProject != nil && !sameProject(catProject, in.ProjectID) {
		return 0, nil, httpx.ErrUnprocessable("提示词与分类的项目范围不一致")
	}
	schemaID, err := s.currentSchemaIDQ(ctx, q, in.CategoryID)
	if err != nil {
		return 0, nil, httpx.WrapError(err)
	}
	var schema map[string]any
	if err := q.QueryRow(ctx, `SELECT schema FROM prompt_schemas WHERE id=$1`, schemaID).Scan(&schema); err != nil {
		return 0, nil, httpx.WrapError(err)
	}
	if err := ValidateContent(schema, in.Content); err != nil {
		return 0, nil, httpx.ErrUnprocessable("内容校验失败: " + err.Error())
	}
	used, err := ValidateVariables(schema, in.Content)
	if err != nil {
		return 0, nil, httpx.ErrUnprocessable(err.Error())
	}
	return schemaID, used, nil
}

// sameProject reports whether two nullable project ids refer to the same scope.
func sameProject(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
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
	// Lock the prompt row so concurrent publishes compute distinct versions.
	if _, err := tx.Exec(ctx, `SELECT id FROM prompts WHERE id=$1 FOR UPDATE`, id); err != nil {
		return promptDTO{}, httpx.WrapError(err)
	}
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

// strSlice returns an empty slice instead of nil so NOT NULL text[] columns
// are not violated.
func strSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
