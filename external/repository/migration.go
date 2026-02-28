package repository

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

var migrationStatements = []string{
	`DO $$ BEGIN CREATE TYPE session_status AS ENUM ('running', 'completed'); EXCEPTION WHEN duplicate_object THEN NULL; END $$`,
	`CREATE TABLE IF NOT EXISTS sessions (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		guild_id TEXT NOT NULL,
		channel_id TEXT NOT NULL,
		started_at TIMESTAMPTZ NOT NULL,
		ended_at TIMESTAMPTZ,
		status session_status NOT NULL DEFAULT 'running'
	)`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_running ON sessions (guild_id, channel_id) WHERE status = 'running'`,
	`CREATE TABLE IF NOT EXISTS transcript_segments (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
		content TEXT NOT NULL,
		segment_index INTEGER NOT NULL,
		spoken_at TIMESTAMPTZ NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE(session_id, segment_index)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_transcript_segments_session ON transcript_segments (session_id, segment_index)`,
}

func RunMigration(ctx context.Context, pool *pgxpool.Pool) error {
	for _, s := range migrationStatements {
		stmt := strings.TrimSpace(s)
		if stmt == "" {
			continue
		}
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
