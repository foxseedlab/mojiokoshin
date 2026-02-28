package session

import (
	"context"
	"testing"
	"time"

	"github.com/foxseedlab/mojiokoshin/internal/audio"
	"github.com/foxseedlab/mojiokoshin/internal/config"
	"github.com/foxseedlab/mojiokoshin/internal/discord"
	"github.com/foxseedlab/mojiokoshin/internal/repository"
	"github.com/foxseedlab/mojiokoshin/internal/transcriber"
)

type mockRepository struct {
	insertCalls []repository.InsertSegmentInput
}

func (m *mockRepository) CreateSession(_ context.Context, _ repository.CreateSessionInput) (*repository.Session, error) {
	return nil, nil
}

func (m *mockRepository) UpdateSessionCompleted(_ context.Context, _ repository.CompleteSessionInput) error {
	return nil
}

func (m *mockRepository) GetRunningSessionByChannel(_ context.Context, _, _ string) (*repository.Session, error) {
	return nil, nil
}

func (m *mockRepository) InsertSegment(_ context.Context, input repository.InsertSegmentInput) error {
	m.insertCalls = append(m.insertCalls, input)
	return nil
}

func (m *mockRepository) ListSegmentsBySessionID(_ context.Context, _ string) ([]repository.TranscriptSegment, error) {
	return nil, nil
}

type mockDiscordClient struct {
	sendCalls []string
}

func (m *mockDiscordClient) Connect(_ context.Context) error { return nil }
func (m *mockDiscordClient) Close() error                    { return nil }
func (m *mockDiscordClient) JoinVoiceChannel(_, _ string) (discord.VoiceConnection, error) {
	return nil, nil
}
func (m *mockDiscordClient) SendChannelMessage(_ string, content string) error {
	m.sendCalls = append(m.sendCalls, content)
	return nil
}
func (m *mockDiscordClient) SendChannelMessageWithFile(_ discord.FileMessage) error { return nil }
func (m *mockDiscordClient) RegisterVoiceStateUpdateHandler(_ func(discord.VoiceStateEvent)) {
}
func (m *mockDiscordClient) Run() error { return nil }

type mockTranscriber struct{}

func (m *mockTranscriber) StartStreaming(_ context.Context, _, _ string, _ transcriber.ResultReceiver) (transcriber.StreamWriter, error) {
	return nil, nil
}

type mockWebhookSender struct{}

func (m *mockWebhookSender) SendTranscript(_ context.Context, _ string, _ []byte) error { return nil }

func newTestManager(repo repository.Repository, dc discord.Client) *Manager {
	cfg := &config.Config{
		DiscordGuildID:            "guild-1",
		DiscordVCID:               "vc-1",
		DefaultTranscribeLanguage: "ja-JP",
		MaxTranscribeDurationMin:  120,
		Env:                       "test",
	}
	return NewManager(cfg, repo, dc, &mockTranscriber{}, &mockWebhookSender{}, func() audio.Mixer { return nil })
}

func TestHandleVoiceStateUpdate_IgnoresOtherGuild(t *testing.T) {
	repo := &mockRepository{}
	dc := &mockDiscordClient{}
	manager := newTestManager(repo, dc)

	manager.HandleVoiceStateUpdate(discord.VoiceStateEvent{
		GuildID:   "guild-2",
		ChannelID: "vc-1",
		UserID:    "user-1",
		Joined:    true,
	})

	if len(repo.insertCalls) != 0 {
		t.Fatalf("expected no repository calls, got %d", len(repo.insertCalls))
	}
	if len(dc.sendCalls) != 0 {
		t.Fatalf("expected no discord calls, got %d", len(dc.sendCalls))
	}
}

func TestHandleTranscriptionResult_InsertsAndSendsOnlyFinalNonEmpty(t *testing.T) {
	repo := &mockRepository{}
	dc := &mockDiscordClient{}
	manager := newTestManager(repo, dc)

	manager.handleTranscriptionResult("session-1", "vc-1", 0, " ", true)
	manager.handleTranscriptionResult("session-1", "vc-1", 0, "hello", false)
	manager.handleTranscriptionResult("session-1", "vc-1", 1, "hello", true)

	if len(repo.insertCalls) != 1 {
		t.Fatalf("expected one insert, got %d", len(repo.insertCalls))
	}
	got := repo.insertCalls[0]
	if got.SessionID != "session-1" || got.Content != "hello" || got.SegmentIndex != 1 {
		t.Fatalf("unexpected insert payload: %+v", got)
	}
	if got.SpokenAt.IsZero() || got.SpokenAt.After(time.Now().Add(1*time.Second)) {
		t.Fatalf("unexpected spoken_at: %v", got.SpokenAt)
	}
	if len(dc.sendCalls) != 1 || dc.sendCalls[0] != "hello" {
		t.Fatalf("unexpected discord sends: %+v", dc.sendCalls)
	}
}

func TestTakeStopReason_ReturnsAndDeletesReason(t *testing.T) {
	repo := &mockRepository{}
	dc := &mockDiscordClient{}
	manager := newTestManager(repo, dc)
	manager.stopReasons["session-1"] = "manual stop"

	reason := manager.takeStopReason("session-1")
	if reason != "manual stop" {
		t.Fatalf("unexpected reason: %s", reason)
	}

	reason = manager.takeStopReason("session-1")
	if reason == "manual stop" {
		t.Fatal("expected reason to be deleted after first read")
	}
}

func TestResultReceiver_OnResultUsesMonotonicIndex(t *testing.T) {
	repo := &mockRepository{}
	dc := &mockDiscordClient{}
	manager := newTestManager(repo, dc)
	receiver := &resultReceiver{
		manager:   manager,
		sessionID: "session-1",
		channelID: "vc-1",
	}

	receiver.OnResult(10, "first", true)
	receiver.OnResult(99, "second", true)

	if len(repo.insertCalls) != 2 {
		t.Fatalf("expected two insert calls, got %d", len(repo.insertCalls))
	}
	if repo.insertCalls[0].SegmentIndex != 0 || repo.insertCalls[1].SegmentIndex != 1 {
		t.Fatalf("unexpected indices: %d, %d", repo.insertCalls[0].SegmentIndex, repo.insertCalls[1].SegmentIndex)
	}
}
