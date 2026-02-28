package webhook

import "context"

const TranscriptWebhookSchemaVersion = "2026-02-28"

type TranscriptWebhookParticipant struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	IsBot       bool   `json:"is_bot"`
}

type TranscriptWebhookSegment struct {
	Index      int    `json:"index"`
	StartAt    string `json:"start_at"`
	EndAt      string `json:"end_at"`
	Transcript string `json:"transcript"`
}

type TranscriptWebhookPayload struct {
	SchemaVersion           string                         `json:"schema_version"`
	SessionID               string                         `json:"session_id"`
	DiscordServerID         string                         `json:"discord_server_id"`
	DiscordServerName       string                         `json:"discord_server_name"`
	DiscordVoiceChannelID   string                         `json:"discord_voice_channel_id"`
	DiscordVoiceChannelName string                         `json:"discord_voice_channel_name"`
	StartAt                 string                         `json:"start_at"`
	EndAt                   string                         `json:"end_at"`
	Timezone                string                         `json:"timezone"`
	DurationSeconds         int64                          `json:"duration_seconds"`
	Participants            []string                       `json:"participants"`
	ParticipantDetails      []TranscriptWebhookParticipant `json:"participant_details"`
	SegmentCount            int                            `json:"segment_count"`
	TranscriptSegments      []TranscriptWebhookSegment     `json:"transcript_segments"`
	Transcript              string                         `json:"transcript"`
}

type Sender interface {
	SendTranscript(ctx context.Context, payload TranscriptWebhookPayload) error
}
