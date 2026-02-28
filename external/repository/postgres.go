package repository

import (
	"context"
	"time"

	"github.com/foxseedlab/mojiokoshin/internal/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) repository.Repository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) CreateSession(ctx context.Context, input repository.CreateSessionInput) (*repository.Session, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO sessions (guild_id, guild_name, channel_id, channel_name, started_at, status)
		 VALUES ($1, $1, $2, $2, $3, 'running')
		 RETURNING id, guild_id, guild_name, channel_id, channel_name, started_at, ended_at, status, stop_reason, timezone, duration_seconds, segment_count, created_at, updated_at`,
		input.GuildID, input.ChannelID, input.StartedAt)
	s, err := scanSession(row)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (r *PostgresRepository) UpdateSessionCompleted(ctx context.Context, input repository.CompleteSessionInput) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE sessions SET status = 'completed', ended_at = $2, updated_at = NOW() WHERE id = $1`,
		input.SessionID, input.EndedAt)
	return err
}

func (r *PostgresRepository) SaveSessionOutput(ctx context.Context, input repository.SaveSessionOutputInput) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	tag, err := tx.Exec(ctx,
		`UPDATE sessions
		 SET status = 'completed',
		     ended_at = $2,
		     stop_reason = $3,
		     guild_name = COALESCE(NULLIF($4, ''), guild_name),
		     channel_name = COALESCE(NULLIF($5, ''), channel_name),
		     timezone = COALESCE(NULLIF($6, ''), timezone),
		     duration_seconds = GREATEST($7, 0),
		     segment_count = GREATEST($8, 0),
		     updated_at = NOW()
		 WHERE id = $1`,
		input.SessionID,
		input.EndedAt,
		input.StopReason,
		input.GuildName,
		input.ChannelName,
		input.Timezone,
		input.DurationSeconds,
		input.SegmentCount,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}

	for _, p := range input.Participants {
		if err := upsertSessionParticipant(ctx, tx, input.SessionID, input.EndedAt, p); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO session_artifacts
			(session_id, transcript_filename, transcript_text, webhook_payload)
		 VALUES ($1, $2, $3, $4::jsonb)
		 ON CONFLICT (session_id) DO UPDATE
		 SET transcript_filename = EXCLUDED.transcript_filename,
		     transcript_text = EXCLUDED.transcript_text,
		     webhook_payload = EXCLUDED.webhook_payload,
		     updated_at = NOW()`,
		input.SessionID,
		input.TranscriptFilename,
		input.TranscriptText,
		nullableJSON(input.WebhookPayloadJSON),
	); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func upsertSessionParticipant(ctx context.Context, tx pgx.Tx, sessionID string, endedAt time.Time, p repository.SessionParticipantSnapshot) error {
	if p.UserID == "" {
		return nil
	}
	firstSeenAt, lastSeenAt := normalizeParticipantSeenAt(p.FirstSeenAt, p.LastSeenAt, endedAt)
	_, err := tx.Exec(ctx,
		`INSERT INTO session_participants
			(session_id, user_id, display_name, is_bot, first_seen_at, last_seen_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (session_id, user_id) DO UPDATE
		 SET display_name = EXCLUDED.display_name,
		     is_bot = EXCLUDED.is_bot,
		     first_seen_at = LEAST(session_participants.first_seen_at, EXCLUDED.first_seen_at),
		     last_seen_at = GREATEST(session_participants.last_seen_at, EXCLUDED.last_seen_at),
		     updated_at = NOW()`,
		sessionID,
		p.UserID,
		fallbackDisplayName(p.DisplayName, p.UserID),
		p.IsBot,
		firstSeenAt,
		lastSeenAt,
	)
	return err
}

func normalizeParticipantSeenAt(firstSeenAt, lastSeenAt, endedAt time.Time) (time.Time, time.Time) {
	if firstSeenAt.IsZero() {
		firstSeenAt = endedAt
	}
	if lastSeenAt.IsZero() {
		lastSeenAt = endedAt
	}
	if lastSeenAt.Before(firstSeenAt) {
		lastSeenAt = firstSeenAt
	}
	return firstSeenAt, lastSeenAt
}

func (r *PostgresRepository) GetRunningSessionByChannel(ctx context.Context, guildID, channelID string) (*repository.Session, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, guild_id, guild_name, channel_id, channel_name, started_at, ended_at, status, stop_reason, timezone, duration_seconds, segment_count, created_at, updated_at
		 FROM sessions WHERE guild_id = $1 AND channel_id = $2 AND status = 'running'
		 LIMIT 1`,
		guildID, channelID)
	s, err := scanSession(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return s, nil
}

func (r *PostgresRepository) InsertSegment(ctx context.Context, input repository.InsertSegmentInput) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO transcript_segments (session_id, content, segment_index, spoken_at)
		 VALUES ($1, $2, $3, $4)`,
		input.SessionID, input.Content, input.SegmentIndex, input.SpokenAt)
	return err
}

func (r *PostgresRepository) ListSegmentsBySessionID(ctx context.Context, sessionID string) ([]repository.TranscriptSegment, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, session_id, content, segment_index, spoken_at, created_at
		 FROM transcript_segments WHERE session_id = $1 ORDER BY segment_index ASC`,
		sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []repository.TranscriptSegment
	for rows.Next() {
		var seg repository.TranscriptSegment
		if err := rows.Scan(&seg.ID, &seg.SessionID, &seg.Content, &seg.SegmentIndex, &seg.SpokenAt, &seg.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, seg)
	}
	return list, rows.Err()
}

type sessionScanner interface {
	Scan(dest ...any) error
}

func scanSession(scanner sessionScanner) (*repository.Session, error) {
	var s repository.Session
	var endedAt *time.Time
	if err := scanner.Scan(
		&s.ID,
		&s.GuildID,
		&s.GuildName,
		&s.ChannelID,
		&s.ChannelName,
		&s.StartedAt,
		&endedAt,
		&s.Status,
		&s.StopReason,
		&s.Timezone,
		&s.DurationSeconds,
		&s.SegmentCount,
		&s.CreatedAt,
		&s.UpdatedAt,
	); err != nil {
		return nil, err
	}
	s.EndedAt = endedAt
	return &s, nil
}

func fallbackDisplayName(displayName, userID string) string {
	if displayName == "" {
		return userID
	}
	return displayName
}

func nullableJSON(v []byte) any {
	if len(v) == 0 {
		return nil
	}
	return string(v)
}
