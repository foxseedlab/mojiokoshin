package repository

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

var migrationStatements = []string{
	`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
	`DO $$ BEGIN CREATE TYPE session_status AS ENUM ('running', 'completed'); EXCEPTION WHEN duplicate_object THEN NULL; END $$`,
	`CREATE TABLE IF NOT EXISTS sessions (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		guild_id TEXT NOT NULL,
		guild_name TEXT NOT NULL DEFAULT '',
		channel_id TEXT NOT NULL,
		channel_name TEXT NOT NULL DEFAULT '',
		started_at TIMESTAMPTZ NOT NULL,
		ended_at TIMESTAMPTZ,
		status session_status NOT NULL DEFAULT 'running',
		stop_reason TEXT NOT NULL DEFAULT '',
		timezone TEXT NOT NULL DEFAULT 'UTC',
		duration_seconds BIGINT NOT NULL DEFAULT 0,
		segment_count INTEGER NOT NULL DEFAULT 0,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS guild_name TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS channel_name TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS stop_reason TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS timezone TEXT NOT NULL DEFAULT 'UTC'`,
	`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS duration_seconds BIGINT NOT NULL DEFAULT 0`,
	`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS segment_count INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`,
	`ALTER TABLE sessions ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`,
	`UPDATE sessions SET guild_name = guild_id WHERE guild_name = ''`,
	`UPDATE sessions SET channel_name = channel_id WHERE channel_name = ''`,
	`UPDATE sessions SET timezone = 'UTC' WHERE timezone = ''`,
	`DO $$ BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conname = 'sessions_duration_seconds_non_negative'
			  AND conrelid = 'sessions'::regclass
		) THEN
			ALTER TABLE sessions ADD CONSTRAINT sessions_duration_seconds_non_negative CHECK (duration_seconds >= 0);
		END IF;
	END $$`,
	`DO $$ BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conname = 'sessions_segment_count_non_negative'
			  AND conrelid = 'sessions'::regclass
		) THEN
			ALTER TABLE sessions ADD CONSTRAINT sessions_segment_count_non_negative CHECK (segment_count >= 0);
		END IF;
	END $$`,
	`WITH ranked_running AS (
		SELECT
			id,
			ROW_NUMBER() OVER (
				PARTITION BY guild_id, channel_id
				ORDER BY started_at DESC, created_at DESC, id DESC
			) AS row_number_in_channel
		FROM sessions
		WHERE status = 'running'
	)
	UPDATE sessions s
	SET
		status = 'completed',
		ended_at = COALESCE(s.ended_at, NOW()),
		stop_reason = CASE
			WHEN s.stop_reason = '' THEN 'auto-completed by running-session uniqueness migration'
			ELSE s.stop_reason
		END,
		updated_at = NOW()
	FROM ranked_running rr
	WHERE s.id = rr.id
	  AND rr.row_number_in_channel > 1`,
	`DROP INDEX IF EXISTS idx_sessions_running`,
	`CREATE UNIQUE INDEX IF NOT EXISTS uq_sessions_running_channel ON sessions (guild_id, channel_id) WHERE status = 'running'`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_history ON sessions (guild_id, channel_id, started_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_started_at ON sessions (started_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_ended_at ON sessions (ended_at DESC) WHERE ended_at IS NOT NULL`,
	`CREATE TABLE IF NOT EXISTS transcript_segments (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
		content TEXT NOT NULL,
		segment_index INTEGER NOT NULL,
		spoken_at TIMESTAMPTZ NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE(session_id, segment_index)
	)`,
	`ALTER TABLE transcript_segments ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`,
	`DO $$ BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conname = 'transcript_segments_segment_index_non_negative'
			  AND conrelid = 'transcript_segments'::regclass
		) THEN
			ALTER TABLE transcript_segments ADD CONSTRAINT transcript_segments_segment_index_non_negative CHECK (segment_index >= 0);
		END IF;
	END $$`,
	`DROP INDEX IF EXISTS idx_transcript_segments_session`,
	`CREATE INDEX IF NOT EXISTS idx_transcript_segments_spoken ON transcript_segments (session_id, spoken_at, segment_index)`,
	`CREATE TABLE IF NOT EXISTS session_participants (
		session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
		user_id TEXT NOT NULL,
		display_name TEXT NOT NULL,
		is_bot BOOLEAN NOT NULL DEFAULT FALSE,
		first_seen_at TIMESTAMPTZ NOT NULL,
		last_seen_at TIMESTAMPTZ NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (session_id, user_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_session_participants_user ON session_participants (user_id, last_seen_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_session_participants_session_bot ON session_participants (session_id, is_bot)`,
	`DO $$ BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM pg_constraint
			WHERE conname = 'session_participants_seen_order'
			  AND conrelid = 'session_participants'::regclass
		) THEN
			ALTER TABLE session_participants ADD CONSTRAINT session_participants_seen_order CHECK (first_seen_at <= last_seen_at);
		END IF;
	END $$`,
	`CREATE TABLE IF NOT EXISTS session_artifacts (
		session_id UUID PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
		transcript_filename TEXT NOT NULL DEFAULT '',
		transcript_text TEXT NOT NULL DEFAULT '',
		webhook_payload JSONB,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_session_artifacts_webhook_payload_gin ON session_artifacts USING GIN (webhook_payload jsonb_path_ops)`,
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
