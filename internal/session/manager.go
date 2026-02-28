package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/foxseedlab/mojiokoshin/internal/audio"
	"github.com/foxseedlab/mojiokoshin/internal/config"
	"github.com/foxseedlab/mojiokoshin/internal/discord"
	"github.com/foxseedlab/mojiokoshin/internal/repository"
	"github.com/foxseedlab/mojiokoshin/internal/transcriber"
	"github.com/foxseedlab/mojiokoshin/internal/webhook"
)

const (
	audioMixInterval = 20 * time.Millisecond
	audioFrameBytes  = 960 * 2 * 2
	stopAllWaitLimit = 15 * time.Second

	commandMojiokoshi     = "mojiokoshi"
	commandMojiokoshiStop = "mojiokoshi-stop"

	stopReasonParticipantsLeft = "all participants left voice channel"
	stopReasonManualSlash      = "stopped by slash command"
	stopReasonMaxDuration      = "maximum transcribe duration exceeded"
	stopReasonBotRemoved       = "transcription bot was removed from voice channel"
	stopReasonServerClosed     = "transcription server closed"
	stopReasonUnknownError     = "unknown error"
)

const (
	StopReasonServerClosed = stopReasonServerClosed
	StopReasonUnknownError = stopReasonUnknownError
)

type Manager struct {
	cfg                *config.Config
	repo               repository.Repository
	discord            discord.Client
	transcriber        transcriber.Transcriber
	webhook            webhook.Sender
	newMixer           audio.MixerFactory
	transcriptLocation *time.Location

	mu          sync.Mutex
	sessions    map[string]*runningSession
	stopReasons map[string]string
	botUserID   string
}

type participantState struct {
	isBot       bool
	firstSeenAt time.Time
	lastSeenAt  time.Time
}

type runningSession struct {
	repoSession        *repository.Session
	voice              discord.VoiceConnection
	mixer              audio.Mixer
	writer             transcriber.StreamWriter
	cancel             context.CancelFunc
	activeParticipants map[string]participantState
	allParticipants    map[string]participantState
}

var slashCommandDefs = []discord.SlashCommandDefinition{
	{
		Name:        commandMojiokoshi,
		Description: slashCommandStartDescription,
	},
	{
		Name:        commandMojiokoshiStop,
		Description: slashCommandStopDescription,
	},
}

func SlashCommandDefinitions() []discord.SlashCommandDefinition {
	defs := make([]discord.SlashCommandDefinition, len(slashCommandDefs))
	copy(defs, slashCommandDefs)
	return defs
}

func NewManager(cfg *config.Config, repo repository.Repository, dc discord.Client, stt transcriber.Transcriber, wh webhook.Sender, newMixer audio.MixerFactory) *Manager {
	loc, err := time.LoadLocation(cfg.TranscriptTimezone)
	if err != nil {
		slog.Warn("failed to load transcript timezone; falling back to UTC", "timezone", cfg.TranscriptTimezone, "error", err)
		loc = time.UTC
	}
	return &Manager{
		cfg:                cfg,
		repo:               repo,
		discord:            dc,
		transcriber:        stt,
		webhook:            wh,
		newMixer:           newMixer,
		transcriptLocation: loc,
		sessions:           make(map[string]*runningSession),
		stopReasons:        make(map[string]string),
	}
}

func (m *Manager) SetBotUserID(botUserID string) {
	botUserID = strings.TrimSpace(botUserID)
	if botUserID == "" {
		return
	}
	m.mu.Lock()
	m.botUserID = botUserID
	m.mu.Unlock()
}

func (m *Manager) HandleSlashCommand(event discord.SlashCommandEvent) {
	slog.Info("slash command received by manager", "guild_id", event.GuildID, "channel_id", event.ChannelID, "command", event.CommandName, "user_id", event.UserID)
	if event.GuildID != m.cfg.DiscordGuildID {
		m.respondEphemeral(event, messageEphemeralWrongGuild)
		return
	}

	switch event.CommandName {
	case commandMojiokoshi:
		m.handleStartCommand(event)
	case commandMojiokoshiStop:
		m.handleStopCommand(event)
	default:
		slog.Warn("unknown slash command received", "command", event.CommandName, "guild_id", event.GuildID, "channel_id", event.ChannelID, "user_id", event.UserID)
		m.respondEphemeral(event, messageEphemeralUnknownCommand)
	}
}

func (m *Manager) handleStartCommand(event discord.SlashCommandEvent) {
	channelID, err := m.discord.GetUserVoiceChannelID(event.GuildID, event.UserID)
	if err != nil {
		slog.Error("failed to resolve user voice channel", "error", err, "guild_id", event.GuildID, "user_id", event.UserID, "command", event.CommandName)
		m.respondEphemeral(event, messageEphemeralVoiceLookupFailed)
		return
	}
	if channelID == "" {
		m.respondEphemeral(event, messageEphemeralJoinVCFirst)
		return
	}
	if m.isSessionRunning(event.GuildID, channelID) {
		m.respondEphemeral(event, messageEphemeralAlreadyRunning)
		return
	}
	if err := m.startSession(event.GuildID, channelID, event.UserID, false); err != nil {
		slog.Error("failed to start session by slash command", "error", err, "guild_id", event.GuildID, "channel_id", channelID, "user_id", event.UserID)
		m.respondEphemeral(event, messageEphemeralStartFailed)
		return
	}
	m.respondEphemeral(event, m.startEphemeralMessage(channelID))
}

func (m *Manager) handleStopCommand(event discord.SlashCommandEvent) {
	channelID, err := m.discord.GetUserVoiceChannelID(event.GuildID, event.UserID)
	if err != nil {
		slog.Error("failed to resolve user voice channel", "error", err, "guild_id", event.GuildID, "user_id", event.UserID, "command", event.CommandName)
		m.respondEphemeral(event, messageEphemeralVoiceLookupFailed)
		return
	}
	if channelID == "" {
		m.respondEphemeral(event, messageEphemeralJoinVCFirst)
		return
	}
	stopped, err := m.stopSession(event.GuildID, channelID, stopReasonManualSlash)
	if err != nil {
		slog.Error("failed to stop session by slash command", "error", err, "guild_id", event.GuildID, "channel_id", channelID, "user_id", event.UserID)
		m.respondEphemeral(event, messageEphemeralStopFailed)
		return
	}
	if !stopped {
		m.respondEphemeral(event, messageEphemeralNotRunning)
		return
	}
	m.respondEphemeral(event, m.stopEphemeralMessage(channelID))
}

func (m *Manager) respondEphemeral(event discord.SlashCommandEvent, content string) {
	if event.RespondEphemeral == nil {
		return
	}
	if err := event.RespondEphemeral(content); err != nil {
		slog.Error("failed to respond ephemeral message", "error", err, "guild_id", event.GuildID, "channel_id", event.ChannelID, "command", event.CommandName, "user_id", event.UserID)
	}
}

func (m *Manager) isSessionRunning(guildID, channelID string) bool {
	key := m.sessionKey(guildID, channelID)
	m.mu.Lock()
	defer m.mu.Unlock()
	_, exists := m.sessions[key]
	return exists
}

func (m *Manager) HandleVoiceStateUpdate(event discord.VoiceStateEvent) {
	slog.Info(
		"voice state update received",
		"guild_id", event.GuildID,
		"user_id", event.UserID,
		"user_is_bot", event.UserIsBot,
		"before_channel_id", event.BeforeChannelID,
		"after_channel_id", event.AfterChannelID,
	)
	if event.GuildID != m.cfg.DiscordGuildID {
		slog.Info("ignoring voice event for different guild", "event_guild_id", event.GuildID, "configured_guild_id", m.cfg.DiscordGuildID)
		return
	}
	if m.handleBotRemovalEvent(event) {
		return
	}
	m.trackVoiceParticipants(event)
	m.startAutoTranscribeIfConfigured(event)
}

func (m *Manager) handleBotRemovalEvent(event discord.VoiceStateEvent) bool {
	if !m.isSelfBotRemovedFromVoiceChannel(event) {
		return false
	}
	if _, err := m.stopSession(event.GuildID, event.BeforeChannelID, stopReasonBotRemoved); err != nil {
		slog.Error("failed to stop session for bot removal", "error", err, "guild_id", event.GuildID, "channel_id", event.BeforeChannelID)
	}
	return true
}

func (m *Manager) trackVoiceParticipants(event discord.VoiceStateEvent) {
	if event.BeforeChannelID == "" && event.AfterChannelID == "" {
		m.removeParticipantFromKnownSessions(event.GuildID, event.UserID, event.UserIsBot)
		return
	}
	if event.BeforeChannelID != "" {
		if err := m.removeParticipantAndMaybeStop(event.GuildID, event.BeforeChannelID, event.UserID, event.UserIsBot); err != nil {
			slog.Error("failed to remove participant", "error", err, "guild_id", event.GuildID, "channel_id", event.BeforeChannelID, "user_id", event.UserID)
		}
	}
	if event.AfterChannelID != "" {
		m.addParticipantIfSessionRunning(event.GuildID, event.AfterChannelID, event.UserID, event.UserIsBot)
	}
}

func (m *Manager) removeParticipantFromKnownSessions(guildID, userID string, userIsBot bool) {
	if strings.TrimSpace(guildID) == "" || strings.TrimSpace(userID) == "" {
		return
	}
	channelIDs := m.findSessionChannelsByActiveParticipant(guildID, userID)
	for _, channelID := range channelIDs {
		if err := m.removeParticipantAndMaybeStop(guildID, channelID, userID, userIsBot); err != nil {
			slog.Error("failed to remove participant from inferred channel", "error", err, "guild_id", guildID, "channel_id", channelID, "user_id", userID)
		}
	}
}

func (m *Manager) findSessionChannelsByActiveParticipant(guildID, userID string) []string {
	prefix := guildID + ":"
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]string, 0, 1)
	for key, rs := range m.sessions {
		if !strings.HasPrefix(key, prefix) || rs == nil {
			continue
		}
		if _, ok := rs.activeParticipants[userID]; !ok {
			continue
		}
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
			continue
		}
		out = append(out, parts[1])
	}
	return out
}

func (m *Manager) startAutoTranscribeIfConfigured(event discord.VoiceStateEvent) {
	if !m.cfg.DiscordAutoTranscribe {
		return
	}
	targetChannelID := m.cfg.DiscordAutoTranscribableVC
	if targetChannelID == "" {
		return
	}

	joinedTarget := event.AfterChannelID == targetChannelID && event.BeforeChannelID != targetChannelID
	if joinedTarget {
		if err := m.startSession(event.GuildID, targetChannelID, event.UserID, event.UserIsBot); err != nil {
			slog.Error("failed to start session", "error", err)
		}
	}
}

func (m *Manager) sessionKey(guildID, channelID string) string {
	return guildID + ":" + channelID
}

func (m *Manager) startSession(guildID, channelID, userID string, userIsBot bool) error {
	if strings.TrimSpace(userID) == "" {
		return nil
	}
	key := m.sessionKey(guildID, channelID)
	slog.Info("start session requested", "session_key", key, "guild_id", guildID, "channel_id", channelID, "user_id", userID, "user_is_bot", userIsBot)

	countable := m.shouldCountLifecycleParticipant(userID, userIsBot)

	m.mu.Lock()
	rs, exists := m.sessions[key]
	if exists {
		m.onSessionAlreadyActive(key, rs, userID, userIsBot, countable)
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	if !countable {
		slog.Info("ignoring join event that does not count toward session lifecycle", "guild_id", guildID, "channel_id", channelID, "user_id", userID, "user_is_bot", userIsBot)
		return nil
	}

	ctx := context.Background()
	if err := m.cleanupOrphanRunningSession(ctx, guildID, channelID); err != nil {
		return err
	}
	created, startedAt, streamCtx, mixer, voice, writer, cancel, err := m.initializeSessionRuntime(ctx, guildID, channelID)
	if err != nil {
		return err
	}

	rs = &runningSession{
		repoSession:        created,
		voice:              voice,
		mixer:              mixer,
		writer:             writer,
		cancel:             cancel,
		activeParticipants: make(map[string]participantState),
		allParticipants:    make(map[string]participantState),
	}
	m.registerSessionJoin(rs, userID, userIsBot, countable, startedAt)

	if participants, err := m.discord.ListVoiceChannelParticipants(guildID, channelID); err != nil {
		slog.Warn("failed to list voice channel participants", "error", err, "guild_id", guildID, "channel_id", channelID)
	} else {
		for _, p := range participants {
			m.registerSessionJoin(rs, p.UserID, p.IsBot, m.shouldCountLifecycleParticipant(p.UserID, p.IsBot), startedAt)
		}
	}

	m.mu.Lock()
	m.sessions[key] = rs
	m.mu.Unlock()
	slog.Info("session activated", "session_key", key, "session_id", created.ID, "active_participants", len(rs.activeParticipants), "all_participants", len(rs.allParticipants))

	_ = m.discord.SendChannelMessage(channelID, m.startChannelMessage())

	var receivedOpusPackets int64
	m.runSessionWorker(guildID, channelID, created.ID, "voice_receive", func() {
		voice.ReceiveAudio(func(audioUserID string, opusPacket []byte) {
			n := atomic.AddInt64(&receivedOpusPackets, 1)
			if n == 1 || n%500 == 0 {
				slog.Info("received opus packet", "session_id", created.ID, "user_id", audioUserID, "packet_bytes", len(opusPacket), "total_packets", n)
			}
			mixer.WriteOpusPacket(audioUserID, opusPacket)
		})
	})
	m.runSessionWorker(guildID, channelID, created.ID, "audio_stream", func() {
		m.streamMixedAudio(streamCtx, created.ID, mixer, writer, &receivedOpusPackets)
	})
	m.runSessionWorker(guildID, channelID, created.ID, "session_timeout_watch", func() {
		m.watchSessionTimeoutForSession(streamCtx, guildID, channelID, created.ID)
	})
	return nil
}

func (m *Manager) runSessionWorker(guildID, channelID, sessionID, workerName string, fn func()) {
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("session worker panicked",
					"panic", recovered,
					"worker", workerName,
					"session_id", sessionID,
					"guild_id", guildID,
					"channel_id", channelID,
				)
				if _, err := m.stopSession(guildID, channelID, stopReasonUnknownError); err != nil {
					slog.Error("failed to stop session after worker panic", "error", err, "session_id", sessionID, "worker", workerName)
				}
			}
		}()
		fn()
	}()
}

func (m *Manager) onSessionAlreadyActive(key string, rs *runningSession, userID string, userIsBot, countable bool) {
	m.registerSessionJoin(rs, userID, userIsBot, countable, time.Now())
	slog.Info("session already active in memory; added participant", "session_key", key, "active_participants", len(rs.activeParticipants), "all_participants", len(rs.allParticipants))
}

func (m *Manager) cleanupOrphanRunningSession(ctx context.Context, guildID, channelID string) error {
	sess, err := m.repo.GetRunningSessionByChannel(ctx, guildID, channelID)
	if err != nil {
		slog.Error("failed to query running session", "error", err, "guild_id", guildID, "channel_id", channelID)
		return err
	}
	if sess == nil {
		return nil
	}
	slog.Warn("found orphan running session in repository; closing and continuing", "session_id", sess.ID, "guild_id", guildID, "channel_id", channelID)
	if err := m.repo.UpdateSessionCompleted(ctx, repository.CompleteSessionInput{SessionID: sess.ID, EndedAt: time.Now()}); err != nil {
		slog.Error("failed to complete orphan session", "error", err, "session_id", sess.ID, "guild_id", guildID, "channel_id", channelID)
		return err
	}
	slog.Info("orphan running session marked as completed", "session_id", sess.ID, "guild_id", guildID, "channel_id", channelID)
	return nil
}

func (m *Manager) initializeSessionRuntime(ctx context.Context, guildID, channelID string) (*repository.Session, time.Time, context.Context, audio.Mixer, discord.VoiceConnection, transcriber.StreamWriter, context.CancelFunc, error) {
	voice, err := m.discord.JoinVoiceChannel(guildID, channelID)
	if err != nil {
		slog.Error("failed to join voice channel", "error", err, "guild_id", guildID, "channel_id", channelID)
		return nil, time.Time{}, nil, nil, nil, nil, nil, err
	}
	slog.Info("joined voice channel", "guild_id", guildID, "channel_id", channelID)

	startedAt := time.Now()
	created, err := m.repo.CreateSession(ctx, repository.CreateSessionInput{GuildID: guildID, ChannelID: channelID, StartedAt: startedAt})
	if err != nil {
		_ = voice.Disconnect()
		slog.Error("failed to create session in repository", "error", err, "guild_id", guildID, "channel_id", channelID)
		return nil, time.Time{}, nil, nil, nil, nil, nil, err
	}
	slog.Info("created session", "session_id", created.ID, "guild_id", guildID, "channel_id", channelID)

	mixer := m.newMixer()
	streamCtx, cancel := context.WithCancel(context.Background())
	receiver := &resultReceiver{manager: m, sessionID: created.ID, channelID: channelID}
	writer, err := m.transcriber.StartStreaming(streamCtx, created.ID, m.cfg.DefaultTranscribeLanguage, receiver)
	if err != nil {
		cancel()
		mixer.Close()
		_ = voice.Disconnect()
		slog.Error("failed to start transcriber streaming", "error", err, "session_id", created.ID)
		return nil, time.Time{}, nil, nil, nil, nil, nil, err
	}
	slog.Info("transcriber streaming started", "session_id", created.ID)
	return created, startedAt, streamCtx, mixer, voice, writer, cancel, nil
}

func (m *Manager) watchSessionTimeoutForSession(ctx context.Context, guildID, channelID, sessionID string) {
	maxDuration := time.Duration(m.cfg.MaxTranscribeDurationMin) * time.Minute
	if maxDuration <= 0 {
		_, _ = m.stopSession(guildID, channelID, stopReasonMaxDuration)
		return
	}
	timer := time.NewTimer(maxDuration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}

	key := m.sessionKey(guildID, channelID)
	m.mu.Lock()
	rs := m.sessions[key]
	stillSameSession := rs != nil && rs.repoSession != nil && rs.repoSession.ID == sessionID
	m.mu.Unlock()
	if !stillSameSession {
		return
	}
	_, err := m.stopSession(guildID, channelID, stopReasonMaxDuration)
	if err != nil {
		slog.Error("failed to stop session by max duration", "error", err, "session_id", sessionID)
	}
}

func (m *Manager) shouldCountLifecycleParticipant(userID string, isBot bool) bool {
	if strings.TrimSpace(userID) == "" {
		return false
	}
	if botUserID := m.getBotUserID(); botUserID != "" && botUserID == userID {
		return false
	}
	if !isBot {
		return true
	}
	return m.cfg.DiscordCountOtherBots
}

func (m *Manager) getBotUserID() string {
	m.mu.Lock()
	botUserID := m.botUserID
	m.mu.Unlock()
	if botUserID != "" {
		return botUserID
	}
	resolved, err := m.discord.GetBotUserID()
	if err != nil {
		return ""
	}
	m.mu.Lock()
	if m.botUserID == "" {
		m.botUserID = strings.TrimSpace(resolved)
	}
	botUserID = m.botUserID
	m.mu.Unlock()
	return botUserID
}

func (m *Manager) isSelfBotRemovedFromVoiceChannel(event discord.VoiceStateEvent) bool {
	if event.BeforeChannelID == "" || event.BeforeChannelID == event.AfterChannelID {
		return false
	}
	botUserID := m.getBotUserID()
	if botUserID == "" {
		return false
	}
	return event.UserID == botUserID
}

func (m *Manager) addParticipantToSession(rs *runningSession, userID string, isBot bool, seenAt time.Time) {
	if strings.TrimSpace(userID) == "" {
		return
	}
	if seenAt.IsZero() {
		seenAt = time.Now()
	}
	current := rs.allParticipants[userID]
	next := participantState{
		isBot:       current.isBot || isBot,
		firstSeenAt: current.firstSeenAt,
		lastSeenAt:  current.lastSeenAt,
	}
	if next.firstSeenAt.IsZero() || seenAt.Before(next.firstSeenAt) {
		next.firstSeenAt = seenAt
	}
	if next.lastSeenAt.IsZero() || seenAt.After(next.lastSeenAt) {
		next.lastSeenAt = seenAt
	}
	rs.allParticipants[userID] = next
}

func (m *Manager) registerSessionJoin(rs *runningSession, userID string, userIsBot, countable bool, seenAt time.Time) {
	m.addParticipantToSession(rs, userID, userIsBot, seenAt)
	if !countable {
		return
	}
	rs.activeParticipants[userID] = participantState{isBot: userIsBot, firstSeenAt: seenAt, lastSeenAt: seenAt}
}

func (m *Manager) addParticipantIfSessionRunning(guildID, channelID, userID string, userIsBot bool) {
	if strings.TrimSpace(channelID) == "" || strings.TrimSpace(userID) == "" {
		return
	}
	key := m.sessionKey(guildID, channelID)
	countable := m.shouldCountLifecycleParticipant(userID, userIsBot)
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	rs, ok := m.sessions[key]
	if !ok {
		return
	}
	m.addParticipantToSession(rs, userID, userIsBot, now)
	if countable {
		rs.activeParticipants[userID] = participantState{isBot: userIsBot, firstSeenAt: now, lastSeenAt: now}
	}
}

func (m *Manager) removeParticipantAndMaybeStop(guildID, channelID, userID string, userIsBot bool) error {
	countable := m.shouldCountLifecycleParticipant(userID, userIsBot)
	key := m.sessionKey(guildID, channelID)
	now := time.Now()
	m.mu.Lock()
	rs, ok := m.sessions[key]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	m.addParticipantToSession(rs, userID, userIsBot, now)
	if countable {
		delete(rs.activeParticipants, userID)
	}
	remaining := len(rs.activeParticipants)
	m.mu.Unlock()
	if remaining > 0 {
		return nil
	}
	_, err := m.stopSession(guildID, channelID, stopReasonParticipantsLeft)
	return err
}

func (m *Manager) streamMixedAudio(ctx context.Context, sessionID string, mixer audio.Mixer, writer transcriber.StreamWriter, receivedOpusPackets *int64) {
	ticker := time.NewTicker(audioMixInterval)
	statsTicker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	defer statsTicker.Stop()
	buf := make([]byte, audioFrameBytes)
	var (
		mixedFrames int64
		zeroFrames  int64
		writeFrames int64
	)
	slog.Info("audio mixer loop started")
	for {
		select {
		case <-ctx.Done():
			slog.Info("audio mixer loop stopped by context cancel",
				"session_id", sessionID,
				"received_opus_packets", atomic.LoadInt64(receivedOpusPackets),
				"mixed_frames", mixedFrames,
				"zero_frames", zeroFrames,
				"written_frames", writeFrames)
			return
		case <-statsTicker.C:
			slog.Info("audio pipeline stats",
				"session_id", sessionID,
				"received_opus_packets", atomic.LoadInt64(receivedOpusPackets),
				"mixed_frames", mixedFrames,
				"zero_frames", zeroFrames,
				"written_frames", writeFrames)
		case <-ticker.C:
			n, err := mixer.ReadMixedPCM(buf)
			if err != nil {
				slog.Warn("failed to read mixed pcm", "error", err, "session_id", sessionID)
				continue
			}
			mixedFrames++
			if n == 0 {
				zeroFrames++
				continue
			}
			if err := writer.Write(buf[:n]); err != nil {
				slog.Error("failed to write pcm to transcriber stream", "error", err, "session_id", sessionID, "pcm_bytes", n)
				return
			}
			writeFrames++
		}
	}
}

func (m *Manager) stopSession(guildID, channelID, reason string) (bool, error) {
	if strings.TrimSpace(reason) == "" {
		reason = stopReasonUnknownError
	}
	rs, ok := m.extractSingleSessionForStop(guildID, channelID, reason)
	if !ok || rs == nil {
		return false, nil
	}
	if rs.repoSession == nil || strings.TrimSpace(rs.repoSession.ID) == "" {
		return false, nil
	}

	endedAt := time.Now()
	slog.Info("stopping session", "session_id", rs.repoSession.ID, "channel_id", channelID, "reason", reason)
	m.terminateSessionRuntime(rs)
	go m.runFinalizeSession(rs, channelID, reason, endedAt)
	return true, nil
}

func (m *Manager) StopAllSessions(reason string) int {
	if strings.TrimSpace(reason) == "" {
		reason = stopReasonUnknownError
	}
	sessions := m.extractAllSessionsForStop(reason)
	if len(sessions) == 0 {
		return 0
	}

	var wg sync.WaitGroup
	for _, stopped := range sessions {
		if stopped.rs == nil || stopped.rs.repoSession == nil || strings.TrimSpace(stopped.rs.repoSession.ID) == "" {
			continue
		}
		wg.Add(1)
		go func(stopped stoppedSession) {
			defer wg.Done()
			m.terminateSessionRuntime(stopped.rs)
			m.runFinalizeSession(stopped.rs, stopped.channelID, reason, time.Now())
		}(stopped)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(stopAllWaitLimit):
		slog.Warn("timed out waiting for session finalization on shutdown", "reason", reason, "session_count", len(sessions), "timeout", stopAllWaitLimit.String())
	}
	return len(sessions)
}

type stoppedSession struct {
	channelID string
	rs        *runningSession
}

func (m *Manager) extractSingleSessionForStop(guildID, channelID, reason string) (*runningSession, bool) {
	key := m.sessionKey(guildID, channelID)
	m.mu.Lock()
	defer m.mu.Unlock()
	rs, ok := m.sessions[key]
	if !ok {
		return nil, false
	}
	delete(m.sessions, key)
	if rs != nil && rs.repoSession != nil {
		m.stopReasons[rs.repoSession.ID] = reason
	}
	return rs, true
}

func (m *Manager) extractAllSessionsForStop(reason string) []stoppedSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]stoppedSession, 0, len(m.sessions))
	for key, rs := range m.sessions {
		channelID := ""
		if rs != nil && rs.repoSession != nil && rs.repoSession.ChannelID != "" {
			channelID = rs.repoSession.ChannelID
		} else {
			parts := strings.SplitN(key, ":", 2)
			if len(parts) == 2 {
				channelID = parts[1]
			}
		}
		out = append(out, stoppedSession{channelID: channelID, rs: rs})
		if rs != nil && rs.repoSession != nil {
			m.stopReasons[rs.repoSession.ID] = reason
		}
		delete(m.sessions, key)
	}
	return out
}

func (m *Manager) terminateSessionRuntime(rs *runningSession) {
	if rs == nil {
		return
	}
	if rs.cancel != nil {
		rs.cancel()
	}
	if rs.writer != nil {
		_ = rs.writer.Close()
	}
	if rs.mixer != nil {
		rs.mixer.Close()
	}
	if rs.voice != nil {
		_ = rs.voice.Disconnect()
	}
}

func (m *Manager) runFinalizeSession(rs *runningSession, channelID, reason string, endedAt time.Time) {
	defer func() {
		if recovered := recover(); recovered != nil {
			sessionID := ""
			if rs != nil && rs.repoSession != nil {
				sessionID = rs.repoSession.ID
			}
			slog.Error("panic while finalizing session", "panic", recovered, "session_id", sessionID, "channel_id", channelID, "reason", reason)
		}
	}()
	m.finalizeSession(rs, channelID, reason, endedAt)
}

func (m *Manager) finalizeSession(rs *runningSession, channelID, reason string, endedAt time.Time) {
	ctx := context.Background()
	if rs == nil || rs.repoSession == nil {
		slog.Warn("skipping finalize for empty session state", "channel_id", channelID, "reason", reason)
		return
	}
	s := rs.repoSession
	segments := m.listSegmentsBestEffort(ctx, s.ID)
	participantUserIDs := participantUserIDsFromStates(rs.allParticipants)
	meta := m.resolveTranscriptMetadataBestEffort(ctx, s, participantUserIDs, rs.allParticipants)
	filename := fmt.Sprintf("transcript-%s.txt", s.ID)
	body := buildTranscriptText(meta, s.StartedAt, endedAt, m.cfg.TranscriptTimezone, m.transcriptLocation, segments)
	m.sendDiscordFinalMessages(s.ID, channelID, reason, filename, body)
	m.completeSessionBestEffort(ctx, s.ID, endedAt)

	payload := buildTranscriptWebhookPayload(s.ID, meta, s.StartedAt, endedAt, m.cfg.TranscriptTimezone, m.transcriptLocation, segments)
	slog.Info("sending transcript webhook payload", "session_id", s.ID, "discord_server_id", payload.DiscordServerID, "discord_server_name", payload.DiscordServerName, "discord_voice_channel_id", payload.DiscordVoiceChannelID, "discord_voice_channel_name", payload.DiscordVoiceChannelName, "segment_count", payload.SegmentCount)
	m.saveSessionOutputBestEffort(ctx, s, reason, endedAt, meta, filename, body, payload, rs.allParticipants)
	m.sendWebhookBestEffort(ctx, s.ID, payload)
}

func (m *Manager) listSegmentsBestEffort(ctx context.Context, sessionID string) []repository.TranscriptSegment {
	segments, err := m.repo.ListSegmentsBySessionID(ctx, sessionID)
	if err != nil {
		slog.Error("failed to list transcript segments", "error", err, "session_id", sessionID)
		return []repository.TranscriptSegment{}
	}
	return segments
}

func participantUserIDsFromStates(states map[string]participantState) []string {
	participantUserIDs := make([]string, 0, len(states))
	for userID := range states {
		participantUserIDs = append(participantUserIDs, userID)
	}
	sort.Strings(participantUserIDs)
	return participantUserIDs
}

func (m *Manager) resolveTranscriptMetadataBestEffort(ctx context.Context, s *repository.Session, participantUserIDs []string, allParticipants map[string]participantState) discord.TranscriptMetadata {
	meta, err := m.discord.ResolveTranscriptMetadata(ctx, s.GuildID, s.ChannelID, participantUserIDs)
	if err != nil {
		slog.Warn("failed to resolve transcript metadata; using fallback values", "error", err, "session_id", s.ID)
		meta = transcriptMetadataFallbackFromSession(s)
	}
	meta = fillTranscriptMetadataFallbacks(meta, s, participantUserIDs, allParticipants)
	slog.Info("resolved transcript metadata", "session_id", s.ID, "discord_server_id", meta.DiscordServerID, "discord_server_name", meta.DiscordServerName, "discord_voice_channel_id", meta.DiscordVoiceChannelID, "discord_voice_channel_name", meta.DiscordVoiceChannelName, "participants", len(meta.Participants))
	return meta
}

func (m *Manager) sendDiscordFinalMessages(sessionID, channelID, reason, filename string, body []byte) {
	if err := m.discord.SendChannelMessage(channelID, m.stopChannelMessage(reason)); err != nil {
		slog.Error("failed to send stop message", "error", err, "session_id", sessionID, "channel_id", channelID, "reason", reason)
	}
	if err := m.discord.SendChannelMessageWithFile(discord.FileMessage{
		ChannelID: channelID,
		Content:   m.transcriptAttachmentMessage(),
		Filename:  filename,
		FileBody:  body,
	}); err != nil {
		slog.Error("failed to send transcript attachment", "error", err, "session_id", sessionID, "channel_id", channelID)
	}
}

func (m *Manager) completeSessionBestEffort(ctx context.Context, sessionID string, endedAt time.Time) {
	if err := m.repo.UpdateSessionCompleted(ctx, repository.CompleteSessionInput{SessionID: sessionID, EndedAt: endedAt}); err != nil {
		slog.Error("failed to complete session", "error", err, "session_id", sessionID)
	}
}

func (m *Manager) saveSessionOutputBestEffort(ctx context.Context, s *repository.Session, reason string, endedAt time.Time, meta discord.TranscriptMetadata, filename string, body []byte, payload webhook.TranscriptWebhookPayload, allParticipants map[string]participantState) {
	participantSnapshots := m.buildParticipantSnapshots(meta, allParticipants, s.StartedAt, endedAt)
	payloadJSON := marshalPayloadBestEffort(payload, s.ID)
	saveInput := repository.SaveSessionOutputInput{
		SessionID:          s.ID,
		EndedAt:            endedAt,
		StopReason:         reason,
		GuildName:          meta.DiscordServerName,
		ChannelName:        meta.DiscordVoiceChannelName,
		Timezone:           m.cfg.TranscriptTimezone,
		DurationSeconds:    payload.DurationSeconds,
		SegmentCount:       payload.SegmentCount,
		Participants:       participantSnapshots,
		TranscriptFilename: filename,
		TranscriptText:     string(body),
		WebhookPayloadJSON: payloadJSON,
	}
	if err := m.repo.SaveSessionOutput(ctx, saveInput); err != nil {
		slog.Error("failed to save session output", "error", err, "session_id", s.ID)
	}
}

func marshalPayloadBestEffort(payload webhook.TranscriptWebhookPayload, sessionID string) []byte {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		slog.Error("failed to marshal webhook payload for database persistence", "error", err, "session_id", sessionID)
		return nil
	}
	return payloadJSON
}

func (m *Manager) sendWebhookBestEffort(ctx context.Context, sessionID string, payload webhook.TranscriptWebhookPayload) {
	if err := m.webhook.SendTranscript(ctx, payload); err != nil {
		slog.Error("failed to send webhook transcript", "error", err, "session_id", sessionID)
	}
}

func (m *Manager) buildParticipantSnapshots(meta discord.TranscriptMetadata, states map[string]participantState, startedAt, endedAt time.Time) []repository.SessionParticipantSnapshot {
	displayByUserID := make(map[string]discord.TranscriptParticipant, len(meta.Participants))
	for _, p := range meta.Participants {
		if strings.TrimSpace(p.UserID) == "" {
			continue
		}
		displayByUserID[p.UserID] = p
	}
	out := make([]repository.SessionParticipantSnapshot, 0, len(states))
	for userID, state := range states {
		firstSeenAt, lastSeenAt := normalizeParticipantBounds(state, startedAt, endedAt)
		name, isBot := participantIdentityForSnapshot(userID, state, displayByUserID)
		out = append(out, repository.SessionParticipantSnapshot{
			UserID:      userID,
			DisplayName: name,
			IsBot:       isBot,
			FirstSeenAt: firstSeenAt,
			LastSeenAt:  lastSeenAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DisplayName != out[j].DisplayName {
			return strings.ToLower(out[i].DisplayName) < strings.ToLower(out[j].DisplayName)
		}
		return out[i].UserID < out[j].UserID
	})
	return out
}

func transcriptMetadataFallbackFromSession(s *repository.Session) discord.TranscriptMetadata {
	return discord.TranscriptMetadata{
		DiscordServerID:         s.GuildID,
		DiscordServerName:       s.GuildID,
		DiscordVoiceChannelID:   s.ChannelID,
		DiscordVoiceChannelName: s.ChannelID,
	}
}

func fillTranscriptMetadataFallbacks(meta discord.TranscriptMetadata, s *repository.Session, participantUserIDs []string, allParticipants map[string]participantState) discord.TranscriptMetadata {
	if meta.DiscordServerID == "" {
		meta.DiscordServerID = s.GuildID
	}
	if meta.DiscordServerName == "" {
		meta.DiscordServerName = s.GuildID
	}
	if meta.DiscordVoiceChannelID == "" {
		meta.DiscordVoiceChannelID = s.ChannelID
	}
	if meta.DiscordVoiceChannelName == "" {
		meta.DiscordVoiceChannelName = s.ChannelID
	}
	if len(meta.Participants) == 0 {
		meta.Participants = fallbackParticipants(participantUserIDs, allParticipants)
	}
	return meta
}

func fallbackParticipants(participantUserIDs []string, allParticipants map[string]participantState) []discord.TranscriptParticipant {
	participants := make([]discord.TranscriptParticipant, 0, len(participantUserIDs))
	for _, userID := range participantUserIDs {
		state := allParticipants[userID]
		participants = append(participants, discord.TranscriptParticipant{
			UserID:      userID,
			DisplayName: userID,
			IsBot:       state.isBot,
		})
	}
	return participants
}

func (m *Manager) startChannelMessage() string {
	lines := []string{
		messageStartChannelTitle,
		messageStartChannelHint,
	}
	return strings.Join(m.withPoweredByForBrand(lines), "\n")
}

func (m *Manager) stopChannelMessage(reason string) string {
	restart := messageStopRestart
	if stopReasonNeedsRestartAgain(reason) {
		restart = messageStopRestartAgain
	}
	lines := []string{
		messageStopChannelTitle,
		"-# " + stopReasonDetail(reason),
		"-# " + restart,
	}
	return strings.Join(lines, "\n")
}

func (m *Manager) transcriptAttachmentMessage() string {
	lines := []string{
		messageAttachmentTitle,
	}
	return strings.Join(m.withPoweredByForBrand(lines), "\n")
}

func (m *Manager) startEphemeralMessage(channelID string) string {
	lines := []string{
		startEphemeralTitle(channelID),
		messageStartEphemeralSecondLine,
		messageStartEphemeralHint,
	}
	return strings.Join(lines, "\n")
}

func (m *Manager) stopEphemeralMessage(channelID string) string {
	lines := []string{
		stopEphemeralTitle(channelID),
		messageStopEphemeralHint,
	}
	return strings.Join(lines, "\n")
}

func (m *Manager) withPoweredByForBrand(lines []string) []string {
	if m.cfg.DiscordShowPoweredBy {
		lines = append(lines, messagePoweredByLine)
	}
	return lines
}

func normalizeParticipantBounds(state participantState, startedAt, endedAt time.Time) (time.Time, time.Time) {
	firstSeenAt := state.firstSeenAt
	if firstSeenAt.IsZero() {
		firstSeenAt = startedAt
	}
	lastSeenAt := state.lastSeenAt
	if lastSeenAt.IsZero() {
		lastSeenAt = endedAt
	}
	if lastSeenAt.Before(firstSeenAt) {
		lastSeenAt = firstSeenAt
	}
	return firstSeenAt, lastSeenAt
}

func participantIdentityForSnapshot(userID string, state participantState, displayByUserID map[string]discord.TranscriptParticipant) (string, bool) {
	name := userID
	isBot := state.isBot
	metaParticipant, ok := displayByUserID[userID]
	if !ok {
		return name, isBot
	}
	if strings.TrimSpace(metaParticipant.DisplayName) != "" {
		name = metaParticipant.DisplayName
	}
	return name, isBot || metaParticipant.IsBot
}

func (m *Manager) handleTranscriptionResult(sessionID, channelID string, segmentIndex int, text string, isFinal bool) {
	if !isFinal || strings.TrimSpace(text) == "" {
		return
	}
	ctx := context.Background()
	if err := m.repo.InsertSegment(ctx, repository.InsertSegmentInput{SessionID: sessionID, Content: text, SegmentIndex: segmentIndex, SpokenAt: time.Now()}); err != nil {
		slog.Error("failed to insert segment", "error", err, "session_id", sessionID)
		return
	}
	if err := m.discord.SendChannelMessage(channelID, text); err != nil {
		slog.Error("failed to post transcript message", "error", err, "session_id", sessionID)
	}
}

type resultReceiver struct {
	manager   *Manager
	sessionID string
	channelID string
	mu        sync.Mutex
	nextIndex int
}

func (r *resultReceiver) OnResult(segmentIndex int, text string, isFinal bool) {
	_ = segmentIndex
	if !isFinal {
		return
	}
	r.mu.Lock()
	idx := r.nextIndex
	r.nextIndex++
	r.mu.Unlock()
	r.manager.handleTranscriptionResult(r.sessionID, r.channelID, idx, text, true)
}

func (r *resultReceiver) OnError(err error) {
	reason := r.manager.takeStopReason(r.sessionID)
	if errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "operation was cancelled") {
		slog.Info("transcriber stream canceled", "error", err, "session_id", r.sessionID, "reason", reason)
		return
	}
	slog.Error("transcriber stream error", "error", err, "session_id", r.sessionID, "reason", reason)
}

func (m *Manager) takeStopReason(sessionID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	reason := m.stopReasons[sessionID]
	delete(m.stopReasons, sessionID)
	if reason == "" {
		return "unknown (likely remote stream close or network interruption)"
	}
	return reason
}
