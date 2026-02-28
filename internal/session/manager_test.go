package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	listSegmentsErr  error
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
	if m.listSegmentsErr != nil {
		return nil, m.listSegmentsErr
	}
	return nil, nil
}

type mockDiscordClient struct {
	sendCalls            []string
	fileCalls            []discord.FileMessage
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
func (m *mockDiscordClient) SendChannelMessageWithFile(msg discord.FileMessage) error {
	m.fileCalls = append(m.fileCalls, msg)
	return nil
}
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
		DiscordShowPoweredBy:       true,
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

	if got != messageEphemeralNotRunning {
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
	if startResp != ":microphone2: <#vc-1> **の文字起こしを開始しました。**\n-# ボイスチャンネルのチャットに文字起こしが表示されます。\n-# /mojiokoshi-stop コマンドで中止できます。" {
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
	if stopResp != ":pause_button:  <#vc-1> **の文字起こしを中止しました。**\n-# /mojiokoshi コマンドで開始できます。" {
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

func TestHandleVoiceStateUpdate_TracksLeaveWhenBeforeChannelIsUnknown(t *testing.T) {
	repo := &mockRepository{}
	dc := &mockDiscordClient{botUserID: "bot-self"}
	manager := newTestManager(repo, dc)
	manager.SetBotUserID("bot-self")
	manager.cfg.DiscordAutoTranscribe = false

	key := manager.sessionKey("guild-1", "vc-1")
	manager.sessions[key] = &runningSession{
		repoSession: &repository.Session{
			ID:        "session-unknown-leave",
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
		BeforeChannelID: "",
		AfterChannelID:  "",
	})

	if manager.isSessionRunning("guild-1", "vc-1") {
		t.Fatal("expected session to stop after unknown-channel leave event")
	}
}

func TestPoweredByShownOnlyOnStartAndAttachment(t *testing.T) {
	repo := &mockRepository{}
	dc := &mockDiscordClient{}
	manager := newTestManager(repo, dc)

	if !strings.Contains(manager.startChannelMessage(), messagePoweredByLine) {
		t.Fatal("expected powered by line on start channel message")
	}
	if !strings.Contains(manager.transcriptAttachmentMessage(), messagePoweredByLine) {
		t.Fatal("expected powered by line on attachment message")
	}
	if strings.Contains(manager.stopChannelMessage(stopReasonManualSlash), messagePoweredByLine) {
		t.Fatal("did not expect powered by line on stop channel message")
	}
	if strings.Contains(manager.startEphemeralMessage("vc-1"), messagePoweredByLine) {
		t.Fatal("did not expect powered by line on start ephemeral message")
	}
	if strings.Contains(manager.stopEphemeralMessage("vc-1"), messagePoweredByLine) {
		t.Fatal("did not expect powered by line on stop ephemeral message")
	}
}

func TestHandleVoiceStateUpdate_BotRemovedStopsSession(t *testing.T) {
	repo := &mockRepository{}
	dc := &mockDiscordClient{botUserID: "bot-self"}
	manager := newTestManager(repo, dc)
	manager.SetBotUserID("bot-self")
	manager.cfg.DiscordAutoTranscribe = false

	key := manager.sessionKey("guild-1", "vc-1")
	manager.sessions[key] = &runningSession{
		repoSession: &repository.Session{
			ID:        "session-bot-removed",
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
		UserID:          "bot-self",
		UserIsBot:       true,
		BeforeChannelID: "vc-1",
		AfterChannelID:  "",
	})

	waitUntil(t, time.Second, func() bool { return !manager.isSessionRunning("guild-1", "vc-1") }, "session should stop when bot is removed")
	waitUntil(t, time.Second, func() bool { return len(dc.fileCalls) == 1 }, "finalize should attach transcript after bot removal")
	if len(dc.sendCalls) == 0 {
		t.Fatal("expected stop message to be sent")
	}
	if dc.sendCalls[0] != manager.stopChannelMessage(stopReasonBotRemoved) {
		t.Fatalf("unexpected stop message: %q", dc.sendCalls[0])
	}
}

func TestStopAllSessions_StopsRunningSessions(t *testing.T) {
	repo := &mockRepository{}
	dc := &mockDiscordClient{}
	manager := newTestManager(repo, dc)

	manager.sessions[manager.sessionKey("guild-1", "vc-1")] = &runningSession{
		repoSession: &repository.Session{
			ID:        "session-stopall-1",
			GuildID:   "guild-1",
			ChannelID: "vc-1",
			StartedAt: time.Now(),
			Status:    repository.SessionStatusRunning,
		},
		activeParticipants: map[string]participantState{"user-1": {isBot: false}},
		allParticipants:    map[string]participantState{"user-1": {isBot: false}},
	}
	manager.sessions[manager.sessionKey("guild-1", "vc-2")] = &runningSession{
		repoSession: &repository.Session{
			ID:        "session-stopall-2",
			GuildID:   "guild-1",
			ChannelID: "vc-2",
			StartedAt: time.Now(),
			Status:    repository.SessionStatusRunning,
		},
		activeParticipants: map[string]participantState{"user-2": {isBot: false}},
		allParticipants:    map[string]participantState{"user-2": {isBot: false}},
	}

	count := manager.StopAllSessions(stopReasonServerClosed)
	if count != 2 {
		t.Fatalf("expected 2 stopped sessions, got %d", count)
	}
	if manager.isSessionRunning("guild-1", "vc-1") || manager.isSessionRunning("guild-1", "vc-2") {
		t.Fatal("expected all sessions to be stopped")
	}
	if len(dc.fileCalls) != 2 {
		t.Fatalf("expected transcript attachments for all sessions, got %d", len(dc.fileCalls))
	}
}

func TestRunSessionWorker_PanicStopsWithUnknownReasonAndSendsAttachment(t *testing.T) {
	repo := &mockRepository{}
	dc := &mockDiscordClient{}
	manager := newTestManager(repo, dc)

	manager.sessions[manager.sessionKey("guild-1", "vc-1")] = &runningSession{
		repoSession: &repository.Session{
			ID:        "session-panic-worker",
			GuildID:   "guild-1",
			ChannelID: "vc-1",
			StartedAt: time.Now(),
			Status:    repository.SessionStatusRunning,
		},
		activeParticipants: map[string]participantState{"user-1": {isBot: false}},
		allParticipants:    map[string]participantState{"user-1": {isBot: false}},
	}

	manager.runSessionWorker("guild-1", "vc-1", "session-panic-worker", "test_worker", func() {
		panic("boom")
	})

	waitUntil(t, time.Second, func() bool { return !manager.isSessionRunning("guild-1", "vc-1") }, "session should stop after worker panic")
	waitUntil(t, time.Second, func() bool { return len(dc.fileCalls) == 1 }, "finalize should attach transcript after worker panic")
	if len(dc.sendCalls) == 0 {
		t.Fatal("expected stop message to be sent")
	}
	if dc.sendCalls[0] != manager.stopChannelMessage(stopReasonUnknownError) {
		t.Fatalf("unexpected stop message: %q", dc.sendCalls[0])
	}
}

func TestFinalizeSession_ContinuesWhenSegmentLookupFails(t *testing.T) {
	repo := &mockRepository{listSegmentsErr: errors.New("boom")}
	dc := &mockDiscordClient{}
	manager := newTestManager(repo, dc)

	manager.sessions[manager.sessionKey("guild-1", "vc-1")] = &runningSession{
		repoSession: &repository.Session{
			ID:        "session-segment-error",
			GuildID:   "guild-1",
			ChannelID: "vc-1",
			StartedAt: time.Now(),
			Status:    repository.SessionStatusRunning,
		},
		activeParticipants: map[string]participantState{"user-1": {isBot: false}},
		allParticipants:    map[string]participantState{"user-1": {isBot: false}},
	}

	stopped, err := manager.stopSession("guild-1", "vc-1", stopReasonUnknownError)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stopped {
		t.Fatal("expected session to be stopped")
	}
	waitUntil(t, time.Second, func() bool { return len(dc.fileCalls) == 1 }, "expected attachment even when segment lookup fails")
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(message)
}
