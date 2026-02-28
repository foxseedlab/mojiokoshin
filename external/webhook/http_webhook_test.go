package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	internalwebhook "github.com/foxseedlab/mojiokoshin/internal/webhook"
)

func TestSendTranscript_EmptyWebhookURL(t *testing.T) {
	sender := NewHTTPSender("")
	if err := sender.SendTranscript(context.Background(), internalwebhook.TranscriptWebhookPayload{}); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestSendTranscript_Success(t *testing.T) {
	var got internalwebhook.TranscriptWebhookPayload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		mediaType := r.Header.Get("Content-Type")
		if !strings.HasPrefix(mediaType, "application/json") {
			t.Fatalf("unexpected content type: %s", mediaType)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	payload := internalwebhook.TranscriptWebhookPayload{
		SchemaVersion:           internalwebhook.TranscriptWebhookSchemaVersion,
		SessionID:               "session-1",
		DiscordServerID:         "guild-1",
		DiscordServerName:       "Guild",
		DiscordVoiceChannelID:   "vc-1",
		DiscordVoiceChannelName: "General",
		StartAt:                 "2026-02-28T12:00:00+09:00",
		EndAt:                   "2026-02-28T12:05:00+09:00",
		Timezone:                "Asia/Tokyo",
		DurationSeconds:         300,
		Participants:            []string{"alice"},
		ParticipantDetails: []internalwebhook.TranscriptWebhookParticipant{
			{
				UserID:      "user-1",
				DisplayName: "alice",
				IsBot:       false,
			},
		},
		SegmentCount: 1,
		TranscriptSegments: []internalwebhook.TranscriptWebhookSegment{
			{
				Index:      0,
				StartAt:    "2026-02-28T12:00:10+09:00",
				EndAt:      "2026-02-28T12:00:20+09:00",
				Transcript: "hello world",
			},
		},
		Transcript: "hello world",
	}

	sender := NewHTTPSender(server.URL)
	if err := sender.SendTranscript(context.Background(), payload); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got.SessionID != "session-1" {
		t.Fatalf("unexpected session_id: %s", got.SessionID)
	}
	if got.DiscordVoiceChannelName != "General" {
		t.Fatalf("unexpected discord_voice_channel_name: %s", got.DiscordVoiceChannelName)
	}
	if len(got.TranscriptSegments) != 1 || got.TranscriptSegments[0].Transcript != "hello world" {
		t.Fatalf("unexpected transcript_segments: %+v", got.TranscriptSegments)
	}
}

func TestSendTranscript_Non2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	sender := NewHTTPSender(server.URL)
	if err := sender.SendTranscript(context.Background(), internalwebhook.TranscriptWebhookPayload{SessionID: "session-1"}); err == nil {
		t.Fatal("expected error for non-2xx response")
	}
}
