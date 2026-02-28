package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
)

type Manager struct {
	cfg         *config.Config
	repo        repository.Repository
	discord     discord.Client
	transcriber transcriber.Transcriber
	webhook     webhook.Sender
	newMixer    audio.MixerFactory

	mu          sync.Mutex
	sessions    map[string]*runningSession
	stopReasons map[string]string
}

type runningSession struct {
	repoSession  *repository.Session
	voice        discord.VoiceConnection
	mixer        audio.Mixer
	writer       transcriber.StreamWriter
	cancel       context.CancelFunc
	participants map[string]struct{}
}

func NewManager(cfg *config.Config, repo repository.Repository, dc discord.Client, stt transcriber.Transcriber, wh webhook.Sender, newMixer audio.MixerFactory) *Manager {
	return &Manager{
		cfg:         cfg,
		repo:        repo,
		discord:     dc,
		transcriber: stt,
		webhook:     wh,
		newMixer:    newMixer,
		sessions:    make(map[string]*runningSession),
		stopReasons: make(map[string]string),
	}
}

func (m *Manager) HandleVoiceStateUpdate(event discord.VoiceStateEvent) {
	slog.Info("voice state update received", "guild_id", event.GuildID, "channel_id", event.ChannelID, "user_id", event.UserID, "joined", event.Joined)
	if event.GuildID != m.cfg.DiscordGuildID {
		slog.Info("ignoring voice event for different guild", "event_guild_id", event.GuildID, "configured_guild_id", m.cfg.DiscordGuildID)
		return
	}
	if event.Joined {
		if event.ChannelID != m.cfg.DiscordVCID {
			slog.Info("ignoring join event for non-target channel", "event_channel_id", event.ChannelID, "configured_channel_id", m.cfg.DiscordVCID)
			return
		}
		if err := m.startSession(event.GuildID, event.ChannelID, event.UserID); err != nil {
			slog.Error("failed to start session", "error", err)
		}
		return
	}
	if err := m.removeParticipantAndMaybeStop(event.GuildID, m.cfg.DiscordVCID, event.UserID); err != nil {
		slog.Error("failed to stop session", "error", err)
	}
}

func (m *Manager) sessionKey(guildID, channelID string) string {
	return guildID + ":" + channelID
}

func (m *Manager) startSession(guildID, channelID, userID string) error {
	key := m.sessionKey(guildID, channelID)
	slog.Info("start session requested", "session_key", key, "guild_id", guildID, "channel_id", channelID, "user_id", userID)

	m.mu.Lock()
	rs, exists := m.sessions[key]
	if exists {
		rs.participants[userID] = struct{}{}
		slog.Info("session already active in memory; added participant", "session_key", key, "participants", len(rs.participants))
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	ctx := context.Background()
	sess, err := m.repo.GetRunningSessionByChannel(ctx, guildID, channelID)
	if err != nil {
		slog.Error("failed to query running session", "error", err, "guild_id", guildID, "channel_id", channelID)
		return err
	}
	if sess != nil {
		slog.Warn("found orphan running session in repository; closing and continuing", "session_id", sess.ID, "guild_id", guildID, "channel_id", channelID)
		if err := m.repo.UpdateSessionCompleted(ctx, repository.CompleteSessionInput{
			SessionID: sess.ID,
			EndedAt:   time.Now(),
		}); err != nil {
			slog.Error("failed to complete orphan session", "error", err, "session_id", sess.ID, "guild_id", guildID, "channel_id", channelID)
			return err
		}
		slog.Info("orphan running session marked as completed", "session_id", sess.ID, "guild_id", guildID, "channel_id", channelID)
	}

	voice, err := m.discord.JoinVoiceChannel(guildID, channelID)
	if err != nil {
		slog.Error("failed to join voice channel", "error", err, "guild_id", guildID, "channel_id", channelID)
		return err
	}
	slog.Info("joined voice channel", "guild_id", guildID, "channel_id", channelID)
	created, err := m.repo.CreateSession(ctx, repository.CreateSessionInput{
		GuildID:   guildID,
		ChannelID: channelID,
		StartedAt: time.Now(),
	})
	if err != nil {
		_ = voice.Disconnect()
		slog.Error("failed to create session in repository", "error", err, "guild_id", guildID, "channel_id", channelID)
		return err
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
		return err
	}
	slog.Info("transcriber streaming started", "session_id", created.ID)

	m.mu.Lock()
	m.sessions[key] = &runningSession{
		repoSession: created,
		voice:       voice,
		mixer:       mixer,
		writer:      writer,
		cancel:      cancel,
		participants: map[string]struct{}{
			userID: {},
		},
	}
	m.mu.Unlock()
	slog.Info("session activated", "session_key", key, "session_id", created.ID, "participants", 1)

	_ = m.discord.SendChannelMessage(channelID, "文字起こしを開始しました。")

	var receivedOpusPackets int64
	go voice.ReceiveAudio(func(userID string, opusPacket []byte) {
		n := atomic.AddInt64(&receivedOpusPackets, 1)
		if n == 1 || n%500 == 0 {
			slog.Info("received opus packet", "session_id", created.ID, "user_id", userID, "packet_bytes", len(opusPacket), "total_packets", n)
		}
		mixer.WriteOpusPacket(userID, opusPacket)
	})
	go m.streamMixedAudio(streamCtx, created.ID, mixer, writer, &receivedOpusPackets)
	return nil
}

func (m *Manager) removeParticipantAndMaybeStop(guildID, channelID, userID string) error {
	key := m.sessionKey(guildID, channelID)
	m.mu.Lock()
	rs, ok := m.sessions[key]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	delete(rs.participants, userID)
	remaining := len(rs.participants)
	m.mu.Unlock()
	if remaining > 0 {
		return nil
	}
	return m.stopSession(guildID, channelID, "all participants left voice channel")
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

func (m *Manager) stopSession(guildID, channelID, reason string) error {
	key := m.sessionKey(guildID, channelID)
	m.mu.Lock()
	rs, ok := m.sessions[key]
	if ok {
		delete(m.sessions, key)
		if rs.repoSession != nil {
			m.stopReasons[rs.repoSession.ID] = reason
		}
	}
	m.mu.Unlock()
	if !ok {
		return nil
	}

	slog.Info("stopping session", "session_id", rs.repoSession.ID, "channel_id", channelID, "reason", reason)
	rs.cancel()
	_ = rs.writer.Close()
	rs.mixer.Close()
	_ = rs.voice.Disconnect()

	go m.finalizeSession(rs.repoSession, channelID)
	return nil
}

func (m *Manager) finalizeSession(s *repository.Session, channelID string) {
	ctx := context.Background()
	segments, err := m.repo.ListSegmentsBySessionID(ctx, s.ID)
	if err != nil {
		slog.Error("failed to list transcript segments", "error", err, "session_id", s.ID)
		return
	}
	lines := make([]string, 0, len(segments))
	for _, seg := range segments {
		lines = append(lines, seg.Content)
	}
	body := []byte(strings.Join(lines, "\n"))
	filename := fmt.Sprintf("transcript-%s.txt", s.ID)
	_ = m.discord.SendChannelMessageWithFile(discord.FileMessage{
		ChannelID: channelID,
		Content:   "文字起こしを終了しました。結果を添付します。",
		Filename:  filename,
		FileBody:  body,
	})
	if err := m.repo.UpdateSessionCompleted(ctx, repository.CompleteSessionInput{
		SessionID: s.ID,
		EndedAt:   time.Now(),
	}); err != nil {
		slog.Error("failed to complete session", "error", err, "session_id", s.ID)
	}
	if err := m.webhook.SendTranscript(ctx, filename, body); err != nil {
		slog.Error("failed to send webhook transcript", "error", err, "session_id", s.ID)
	}
}

func (m *Manager) handleTranscriptionResult(sessionID, channelID string, segmentIndex int, text string, isFinal bool) {
	if !isFinal || strings.TrimSpace(text) == "" {
		return
	}
	ctx := context.Background()
	if err := m.repo.InsertSegment(ctx, repository.InsertSegmentInput{
		SessionID:    sessionID,
		Content:      text,
		SegmentIndex: segmentIndex,
		SpokenAt:     time.Now(),
	}); err != nil {
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
