package session

import (
	"strings"
	"testing"
	"time"

	"github.com/foxseedlab/mojiokoshin/internal/discord"
	"github.com/foxseedlab/mojiokoshin/internal/repository"
	"github.com/foxseedlab/mojiokoshin/internal/webhook"
)

func TestBuildTranscriptText(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}
	startedAt := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)
	endedAt := startedAt.Add(2 * time.Minute)
	segments := []repository.TranscriptSegment{
		{SegmentIndex: 0, SpokenAt: startedAt.Add(15 * time.Second), Content: "こんにちは"},
		{SegmentIndex: 1, SpokenAt: startedAt.Add(75 * time.Second), Content: "よろしくお願いします"},
	}

	body := string(buildTranscriptText(discord.TranscriptMetadata{
		DiscordServerName:       "Kemo Server",
		DiscordVoiceChannelName: "General VC",
		Participants: []discord.TranscriptParticipant{
			{UserID: "u2", DisplayName: "Bob"},
			{UserID: "u1", DisplayName: "Alice"},
		},
	}, startedAt, endedAt, "Asia/Tokyo", loc, segments))

	if !strings.Contains(body, "サーバー名：Kemo Server") {
		t.Fatalf("server name not found in body: %s", body)
	}
	if !strings.Contains(body, "ボイスチャンネル名：General VC") {
		t.Fatalf("channel name not found in body: %s", body)
	}
	if !strings.Contains(body, "参加者：Alice、Bob") {
		t.Fatalf("participants line not found in body: %s", body)
	}
	if !strings.Contains(body, "00:00:15 こんにちは") {
		t.Fatalf("first segment line not found in body: %s", body)
	}
	if !strings.Contains(body, "00:01:15 よろしくお願いします") {
		t.Fatalf("second segment line not found in body: %s", body)
	}
}

func TestBuildTranscriptWebhookPayload_SegmentEndAtRules(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}
	startedAt := time.Date(2026, 2, 28, 19, 0, 0, 0, loc)
	segments := []repository.TranscriptSegment{
		{SegmentIndex: 0, SpokenAt: startedAt.Add(10 * time.Second), Content: "first"},
		{SegmentIndex: 1, SpokenAt: startedAt.Add(30 * time.Second), Content: "second"},
	}
	endedAt := startedAt.Add(45 * time.Second)

	payload := buildTranscriptWebhookPayload("session-1", discord.TranscriptMetadata{
		DiscordServerID:         "guild-1",
		DiscordServerName:       "guild",
		DiscordVoiceChannelID:   "vc-1",
		DiscordVoiceChannelName: "vc",
		Participants: []discord.TranscriptParticipant{
			{UserID: "u2", DisplayName: "bob"},
			{UserID: "u1", DisplayName: "alice"},
		},
	}, startedAt, endedAt, "Asia/Tokyo", loc, segments)

	assertTranscriptPayloadCore(t, payload, segments, endedAt)
	assertTranscriptPayloadMetadata(t, payload)
}

func assertTranscriptPayloadCore(t *testing.T, payload webhook.TranscriptWebhookPayload, segments []repository.TranscriptSegment, endedAt time.Time) {
	if payload.SchemaVersion != "2026-02-28" {
		t.Fatalf("unexpected schema_version: %s", payload.SchemaVersion)
	}
	if len(payload.TranscriptSegments) != 2 {
		t.Fatalf("unexpected transcript segment count: %d", len(payload.TranscriptSegments))
	}
	if payload.TranscriptSegments[0].EndAt != segments[1].SpokenAt.Format(time.RFC3339) {
		t.Fatalf("unexpected first segment end_at: %s", payload.TranscriptSegments[0].EndAt)
	}
	if payload.TranscriptSegments[1].EndAt != endedAt.Format(time.RFC3339) {
		t.Fatalf("unexpected second segment end_at: %s", payload.TranscriptSegments[1].EndAt)
	}
	if payload.Participants[0] != "alice" || payload.Participants[1] != "bob" {
		t.Fatalf("participants are not sorted: %+v", payload.Participants)
	}
}

func assertTranscriptPayloadMetadata(t *testing.T, payload webhook.TranscriptWebhookPayload) {
	if payload.DiscordServerID != "guild-1" || payload.DiscordServerName != "guild" {
		t.Fatalf("unexpected discord server fields: id=%s name=%s", payload.DiscordServerID, payload.DiscordServerName)
	}
	if payload.DiscordVoiceChannelID != "vc-1" || payload.DiscordVoiceChannelName != "vc" {
		t.Fatalf("unexpected discord channel fields: id=%s name=%s", payload.DiscordVoiceChannelID, payload.DiscordVoiceChannelName)
	}
	if payload.ParticipantDetails[0].DisplayName != "alice" || payload.ParticipantDetails[1].DisplayName != "bob" {
		t.Fatalf("participant details are not sorted or missing names: %+v", payload.ParticipantDetails)
	}
	if payload.Timezone != "Asia/Tokyo" {
		t.Fatalf("unexpected timezone: %s", payload.Timezone)
	}
}
