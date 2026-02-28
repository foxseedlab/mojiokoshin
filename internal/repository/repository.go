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

type SessionParticipantSnapshot struct {
	UserID      string
	DisplayName string
	IsBot       bool
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}

type SaveSessionOutputInput struct {
	SessionID          string
	EndedAt            time.Time
	StopReason         string
	GuildName          string
	ChannelName        string
	Timezone           string
	DurationSeconds    int64
	SegmentCount       int
	Participants       []SessionParticipantSnapshot
	TranscriptFilename string
	TranscriptText     string
	WebhookPayloadJSON []byte
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
	SaveSessionOutput(ctx context.Context, input SaveSessionOutputInput) error
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
