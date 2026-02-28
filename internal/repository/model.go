package repository

import "time"

type SessionStatus string

const (
	SessionStatusRunning   SessionStatus = "running"
	SessionStatusCompleted SessionStatus = "completed"
)

type Session struct {
	ID        string
	GuildID   string
	ChannelID string
	StartedAt time.Time
	EndedAt   *time.Time
	Status    SessionStatus
}

type TranscriptSegment struct {
	ID           string
	SessionID    string
	Content      string
	SegmentIndex int
	SpokenAt     time.Time
	CreatedAt    time.Time
}
