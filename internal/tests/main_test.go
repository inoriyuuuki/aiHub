//go:build integration

package tests

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMain cleans all AIHub tables before each test run so repeated runs are
// deterministic.
func TestMain(m *testing.M) {
	dsn := os.Getenv("AIHUB_TEST_DATABASE_URL")
	if dsn != "" {
		pool, err := pgxpool.New(context.Background(), dsn)
		if err == nil {
			_, _ = pool.Exec(context.Background(), `
				TRUNCATE audit_log, sessions, api_tokens, users, projects,
				         prompt_versions, prompts, prompt_schemas, prompt_categories, assets,
				         skill_versions, skills,
				         expert_members, expert_pack_versions, expert_packs,
				         mcp_profile_items, mcp_profiles, mcp_definition_versions, mcp_definitions
				RESTART IDENTITY CASCADE`)
			pool.Close()
		}
	}
	os.Exit(m.Run())
}
