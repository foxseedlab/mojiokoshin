package repository

import (
	"context"
	"time"
)

type CreateSessionInput struct {
	GuildID   string
	ChannelID string
	StartedAt time.Time
}

type CompleteSessionInput struct {
	SessionID string
	EndedAt   time.Time
}

type InsertSegmentInput struct {
	SessionID    string
	Content      string
	SegmentIndex int
	SpokenAt     time.Time
}

type SessionRepository interface {
	CreateSession(ctx context.Context, input CreateSessionInput) (*Session, error)
	UpdateSessionCompleted(ctx context.Context, input CompleteSessionInput) error
	GetRunningSessionByChannel(ctx context.Context, guildID, channelID string) (*Session, error)
}

type TranscriptRepository interface {
	InsertSegment(ctx context.Context, input InsertSegmentInput) error
	ListSegmentsBySessionID(ctx context.Context, sessionID string) ([]TranscriptSegment, error)
}

type Repository interface {
	SessionRepository
	TranscriptRepository
}
