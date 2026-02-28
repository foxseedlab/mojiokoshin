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
		`INSERT INTO sessions (guild_id, channel_id, started_at, status)
		 VALUES ($1, $2, $3, 'running')
		 RETURNING id, guild_id, channel_id, started_at, ended_at, status`,
		input.GuildID, input.ChannelID, input.StartedAt)
	var s repository.Session
	var endedAt *time.Time
	err := row.Scan(&s.ID, &s.GuildID, &s.ChannelID, &s.StartedAt, &endedAt, &s.Status)
	if err != nil {
		return nil, err
	}
	s.EndedAt = endedAt
	return &s, nil
}

func (r *PostgresRepository) UpdateSessionCompleted(ctx context.Context, input repository.CompleteSessionInput) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE sessions SET status = 'completed', ended_at = $2 WHERE id = $1`,
		input.SessionID, input.EndedAt)
	return err
}

func (r *PostgresRepository) GetRunningSessionByChannel(ctx context.Context, guildID, channelID string) (*repository.Session, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, guild_id, channel_id, started_at, ended_at, status
		 FROM sessions WHERE guild_id = $1 AND channel_id = $2 AND status = 'running'
		 LIMIT 1`,
		guildID, channelID)
	var s repository.Session
	var endedAt *time.Time
	err := row.Scan(&s.ID, &s.GuildID, &s.ChannelID, &s.StartedAt, &endedAt, &s.Status)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	s.EndedAt = endedAt
	return &s, nil
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
