// Package db provides PostgreSQL access and module-level migrations.
package db

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool is a thin wrapper around pgxpool.Pool with migration support.
type Pool struct {
	*pgxpool.Pool
}

// Open creates a connection pool and verifies connectivity.
func Open(ctx context.Context, dsn string) (*Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := pool.Ping(pctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Pool{Pool: pool}, nil
}

// Migration is a single module-level SQL migration.
type Migration struct {
	// ID is a unique, monotonically ordered identifier like "20260809001_init".
	ID  string
	SQL string
	// FS is an optional embed.FS and File the path inside it. Either SQL or FS+File must be set.
	FS   *embed.FS
	File string
}

// Migrate applies all migrations under an advisory lock so concurrent
// instances only execute them once.
func Migrate(ctx context.Context, p *Pool, migrations []Migration) error {
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].ID < migrations[j].ID })
	// Single database-wide advisory lock keyed to a fixed constant.
	if _, err := p.Exec(ctx, `SELECT pg_advisory_lock(83451023815)`); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer p.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(83451023815)`) //nolint:errcheck

	if _, err := p.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		id text PRIMARY KEY,
		applied_at timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	for _, m := range migrations {
		var exists bool
		if err := p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE id=$1)`, m.ID).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %s: %w", m.ID, err)
		}
		if exists {
			continue
		}
		sqlText := m.SQL
		if sqlText == "" && m.FS != nil && m.File != "" {
			data, err := m.FS.ReadFile(m.File)
			if err != nil {
				return fmt.Errorf("read migration %s: %w", m.ID, err)
			}
			sqlText = string(data)
		}
		if sqlText == "" {
			return fmt.Errorf("migration %s has no SQL", m.ID)
		}
		tx, err := p.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, sqlText); err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			return fmt.Errorf("apply migration %s: %w", m.ID, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (id) VALUES ($1)`, m.ID); err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			return fmt.Errorf("record migration %s: %w", m.ID, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", m.ID, err)
		}
	}
	return nil
}
