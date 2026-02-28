package repository

import "time"

type SessionStatus string

const (
	SessionStatusRunning   SessionStatus = "running"
	SessionStatusCompleted SessionStatus = "completed"
)

type Session struct {
	ID              string
	GuildID         string
	GuildName       string
	ChannelID       string
	ChannelName     string
	StartedAt       time.Time
	EndedAt         *time.Time
	Status          SessionStatus
	StopReason      string
	Timezone        string
	DurationSeconds int64
	SegmentCount    int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type TranscriptSegment struct {
	ID           string
	SessionID    string
	Content      string
	SegmentIndex int
	SpokenAt     time.Time
	CreatedAt    time.Time
}

type SessionParticipant struct {
	SessionID   string
	UserID      string
	DisplayName string
	IsBot       bool
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}
