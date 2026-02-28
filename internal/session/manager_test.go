package session

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/foxseedlab/mojiokoshin/internal/audio"
	"github.com/foxseedlab/mojiokoshin/internal/config"
	"github.com/foxseedlab/mojiokoshin/internal/discord"
	"github.com/foxseedlab/mojiokoshin/internal/repository"
	"github.com/foxseedlab/mojiokoshin/internal/transcriber"
	"github.com/foxseedlab/mojiokoshin/internal/webhook"
)

type mockRepository struct {
	insertCalls      []repository.InsertSegmentInput
	savedOutputCalls []repository.SaveSessionOutputInput
	createCount      int
}

func (m *mockRepository) CreateSession(_ context.Context, input repository.CreateSessionInput) (*repository.Session, error) {
	m.createCount++
	return &repository.Session{
		ID:        fmt.Sprintf("session-%d", m.createCount),
		GuildID:   input.GuildID,
		ChannelID: input.ChannelID,
		StartedAt: input.StartedAt,
		Status:    repository.SessionStatusRunning,
	}, nil
}

func (m *mockRepository) UpdateSessionCompleted(_ context.Context, _ repository.CompleteSessionInput) error {
	return nil
}

func (m *mockRepository) SaveSessionOutput(_ context.Context, input repository.SaveSessionOutputInput) error {
	m.savedOutputCalls = append(m.savedOutputCalls, input)
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
	sendCalls            []string
	userVoiceChannelByID map[string]string
	botUserID            string
}

func (m *mockDiscordClient) Connect(_ context.Context) error { return nil }
func (m *mockDiscordClient) Close() error                    { return nil }
func (m *mockDiscordClient) JoinVoiceChannel(_, _ string) (discord.VoiceConnection, error) {
	return &mockVoiceConnection{}, nil
}
func (m *mockDiscordClient) SendChannelMessage(_ string, content string) error {
	m.sendCalls = append(m.sendCalls, content)
	return nil
}
func (m *mockDiscordClient) SendChannelMessageWithFile(_ discord.FileMessage) error { return nil }
func (m *mockDiscordClient) RegisterVoiceStateUpdateHandler(_ func(discord.VoiceStateEvent)) {
}
func (m *mockDiscordClient) RegisterSlashCommandHandler(_ func(discord.SlashCommandEvent)) {}
func (m *mockDiscordClient) UpsertGuildSlashCommands(_ string, _ []discord.SlashCommandDefinition) error {
	return nil
}
func (m *mockDiscordClient) GetUserVoiceChannelID(_, userID string) (string, error) {
	if m.userVoiceChannelByID == nil {
		return "", nil
	}
	return m.userVoiceChannelByID[userID], nil
}
func (m *mockDiscordClient) ListVoiceChannelParticipants(_, _ string) ([]discord.VoiceParticipant, error) {
	return nil, nil
}
func (m *mockDiscordClient) GetBotUserID() (string, error) {
	if m.botUserID != "" {
		return m.botUserID, nil
	}
	return "bot-self", nil
}
func (m *mockDiscordClient) ResolveTranscriptMetadata(_ context.Context, guildID, channelID string, participantUserIDs []string) (discord.TranscriptMetadata, error) {
	participants := make([]discord.TranscriptParticipant, 0, len(participantUserIDs))
	for _, userID := range participantUserIDs {
		participants = append(participants, discord.TranscriptParticipant{UserID: userID, DisplayName: userID})
	}
	return discord.TranscriptMetadata{
		DiscordServerID:         guildID,
		DiscordServerName:       guildID,
		DiscordVoiceChannelID:   channelID,
		DiscordVoiceChannelName: channelID,
		Participants:            participants,
	}, nil
}
func (m *mockDiscordClient) Run() error { return nil }

type mockTranscriber struct{}

func (m *mockTranscriber) StartStreaming(_ context.Context, _, _ string, _ transcriber.ResultReceiver) (transcriber.StreamWriter, error) {
	return &mockStreamWriter{}, nil
}

type mockStreamWriter struct{}

func (m *mockStreamWriter) Write(_ []byte) error { return nil }
func (m *mockStreamWriter) Close() error         { return nil }

type mockVoiceConnection struct{}

func (m *mockVoiceConnection) Disconnect() error { return nil }
func (m *mockVoiceConnection) ReceiveAudio(_ func(userID string, opusPCM []byte)) {
}

type mockWebhookSender struct{}

func (m *mockWebhookSender) SendTranscript(_ context.Context, _ webhook.TranscriptWebhookPayload) error {
	return nil
}

type mockMixer struct{}

func (m *mockMixer) WriteOpusPacket(_ string, _ []byte) {}
func (m *mockMixer) ReadMixedPCM(_ []byte) (int, error) { return 0, nil }
func (m *mockMixer) Close()                             {}

func newTestManager(repo repository.Repository, dc discord.Client) *Manager {
	cfg := &config.Config{
		DiscordGuildID:             "guild-1",
		DiscordAutoTranscribe:      true,
		DiscordAutoTranscribableVC: "vc-1",
		DiscordCountOtherBots:      false,
		TranscriptTimezone:         "Asia/Tokyo",
		DefaultTranscribeLanguage:  "ja-JP",
		MaxTranscribeDurationMin:   120,
		Env:                        "test",
	}
	return NewManager(cfg, repo, dc, &mockTranscriber{}, &mockWebhookSender{}, func() audio.Mixer { return &mockMixer{} })
}

func TestHandleVoiceStateUpdate_IgnoresOtherGuild(t *testing.T) {
	repo := &mockRepository{}
	dc := &mockDiscordClient{}
	manager := newTestManager(repo, dc)

	manager.HandleVoiceStateUpdate(discord.VoiceStateEvent{
		GuildID:         "guild-2",
		BeforeChannelID: "",
		AfterChannelID:  "vc-1",
		UserID:          "user-1",
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

func TestHandleSlashCommand_StartRequiresVC(t *testing.T) {
	repo := &mockRepository{}
	dc := &mockDiscordClient{}
	manager := newTestManager(repo, dc)
	var got string

	manager.HandleSlashCommand(discord.SlashCommandEvent{
		GuildID:     "guild-1",
		CommandName: commandMojiokoshi,
		UserID:      "user-1",
		RespondEphemeral: func(content string) error {
			got = content
			return nil
		},
	})

	if got == "" {
		t.Fatal("expected an ephemeral error response")
	}
}

func TestHandleSlashCommand_StopReturnsNotRunning(t *testing.T) {
	repo := &mockRepository{}
	dc := &mockDiscordClient{
		userVoiceChannelByID: map[string]string{"user-1": "vc-1"},
	}
	manager := newTestManager(repo, dc)
	var got string

	manager.HandleSlashCommand(discord.SlashCommandEvent{
		GuildID:     "guild-1",
		CommandName: commandMojiokoshiStop,
		UserID:      "user-1",
		RespondEphemeral: func(content string) error {
			got = content
			return nil
		},
	})

	if got != "現在このVCでは文字起こしは実行されていません。" {
		t.Fatalf("unexpected response: %q", got)
	}
}

func TestHandleSlashCommand_StartAndStopSuccess(t *testing.T) {
	repo := &mockRepository{}
	dc := &mockDiscordClient{
		userVoiceChannelByID: map[string]string{"user-1": "vc-1"},
	}
	manager := newTestManager(repo, dc)
	manager.SetBotUserID("bot-self")

	var startResp string
	manager.HandleSlashCommand(discord.SlashCommandEvent{
		GuildID:     "guild-1",
		CommandName: commandMojiokoshi,
		UserID:      "user-1",
		RespondEphemeral: func(content string) error {
			startResp = content
			return nil
		},
	})
	if startResp != "文字起こしを開始しました。" {
		t.Fatalf("unexpected start response: %q", startResp)
	}
	if !manager.isSessionRunning("guild-1", "vc-1") {
		t.Fatal("expected running session after start command")
	}

	var stopResp string
	manager.HandleSlashCommand(discord.SlashCommandEvent{
		GuildID:     "guild-1",
		CommandName: commandMojiokoshiStop,
		UserID:      "user-1",
		RespondEphemeral: func(content string) error {
			stopResp = content
			return nil
		},
	})
	if stopResp != "文字起こしを停止しました。" {
		t.Fatalf("unexpected stop response: %q", stopResp)
	}
	if manager.isSessionRunning("guild-1", "vc-1") {
		t.Fatal("expected session to stop after stop command")
	}
}

func TestShouldCountLifecycleParticipant_ExcludesSelfBotAlways(t *testing.T) {
	repo := &mockRepository{}
	dc := &mockDiscordClient{botUserID: "bot-self"}
	manager := newTestManager(repo, dc)
	manager.SetBotUserID("bot-self")

	if manager.shouldCountLifecycleParticipant("bot-self", true) {
		t.Fatal("expected own bot user to be excluded from lifecycle participant count")
	}
}

func TestShouldCountLifecycleParticipant_OtherBotsControlledByConfig(t *testing.T) {
	repo := &mockRepository{}
	dc := &mockDiscordClient{botUserID: "bot-self"}
	manager := newTestManager(repo, dc)
	manager.SetBotUserID("bot-self")

	if manager.shouldCountLifecycleParticipant("other-bot", true) {
		t.Fatal("expected other bot to be excluded by default")
	}
	manager.cfg.DiscordCountOtherBots = true
	if !manager.shouldCountLifecycleParticipant("other-bot", true) {
		t.Fatal("expected other bot to be counted when config is enabled")
	}
}

func TestStopSession_MaxDurationReasonRemovesSession(t *testing.T) {
	repo := &mockRepository{}
	dc := &mockDiscordClient{}
	manager := newTestManager(repo, dc)

	key := manager.sessionKey("guild-1", "vc-1")
	manager.sessions[key] = &runningSession{
		repoSession:        &repository.Session{ID: "session-1", GuildID: "guild-1", ChannelID: "vc-1", StartedAt: time.Now()},
		activeParticipants: map[string]participantState{"user-1": {isBot: false}},
		allParticipants:    map[string]participantState{"user-1": {isBot: false}},
	}

	stopped, err := manager.stopSession("guild-1", "vc-1", stopReasonMaxDuration)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stopped {
		t.Fatal("expected session to be stopped")
	}

	if _, ok := manager.sessions[key]; ok {
		t.Fatal("expected session to be removed after stop")
	}
}

func TestRemoveParticipantAndMaybeStop_WhenOnlySelfBotRemains(t *testing.T) {
	repo := &mockRepository{}
	dc := &mockDiscordClient{botUserID: "bot-self"}
	manager := newTestManager(repo, dc)
	manager.SetBotUserID("bot-self")

	key := manager.sessionKey("guild-1", "vc-1")
	manager.sessions[key] = &runningSession{
		repoSession: &repository.Session{
			ID:        "session-1",
			GuildID:   "guild-1",
			ChannelID: "vc-1",
			StartedAt: time.Now(),
			Status:    repository.SessionStatusRunning,
		},
		activeParticipants: map[string]participantState{
			"user-1": {isBot: false},
		},
		allParticipants: map[string]participantState{
			"user-1":   {isBot: false},
			"bot-self": {isBot: true},
		},
	}

	if err := manager.removeParticipantAndMaybeStop("guild-1", "vc-1", "user-1", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manager.isSessionRunning("guild-1", "vc-1") {
		t.Fatal("expected session to stop when only self bot remains")
	}
}

func TestHandleVoiceStateUpdate_TracksLeaveEvenWhenAutoDisabled(t *testing.T) {
	repo := &mockRepository{}
	dc := &mockDiscordClient{botUserID: "bot-self"}
	manager := newTestManager(repo, dc)
	manager.SetBotUserID("bot-self")
	manager.cfg.DiscordAutoTranscribe = false

	key := manager.sessionKey("guild-1", "vc-1")
	manager.sessions[key] = &runningSession{
		repoSession: &repository.Session{
			ID:        "session-2",
			GuildID:   "guild-1",
			ChannelID: "vc-1",
			StartedAt: time.Now(),
			Status:    repository.SessionStatusRunning,
		},
		activeParticipants: map[string]participantState{
			"user-1": {isBot: false},
		},
		allParticipants: map[string]participantState{
			"user-1":   {isBot: false},
			"bot-self": {isBot: true},
		},
	}

	manager.HandleVoiceStateUpdate(discord.VoiceStateEvent{
		GuildID:         "guild-1",
		UserID:          "user-1",
		UserIsBot:       false,
		BeforeChannelID: "vc-1",
		AfterChannelID:  "",
	})

	if manager.isSessionRunning("guild-1", "vc-1") {
		t.Fatal("expected session to stop after participant leaves even when auto is disabled")
	}
}
