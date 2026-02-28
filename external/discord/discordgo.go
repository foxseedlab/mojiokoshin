package discord

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
	discordpkg "github.com/foxseedlab/mojiokoshin/internal/discord"
)

type Client struct {
	session   *discordgo.Session
	token     string
	botUserID string
}

func NewClient(token string) discordpkg.Client {
	return &Client{
		token: token,
	}
}

func (c *Client) Connect(ctx context.Context) error {
	_ = ctx
	s, err := discordgo.New("Bot " + c.token)
	if err != nil {
		return err
	}
	c.session = s
	s.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsGuildVoiceStates)
	s.State.TrackVoice = true
	if err := s.Open(); err != nil {
		return err
	}
	userID, err := c.GetBotUserID()
	if err != nil {
		return err
	}
	c.botUserID = userID
	return nil
}

func (c *Client) Close() error {
	if c.session != nil {
		return c.session.Close()
	}
	return nil
}

func (c *Client) JoinVoiceChannel(guildID, channelID string) (discordpkg.VoiceConnection, error) {
	vc, err := c.session.ChannelVoiceJoin(guildID, channelID, false, false)
	if err != nil {
		return nil, err
	}
	return &voiceConnectionImpl{vc: vc}, nil
}

func (c *Client) SendChannelMessage(channelID, content string) error {
	_, err := c.session.ChannelMessageSend(channelID, content)
	return err
}

func (c *Client) SendChannelMessageWithFile(msg discordpkg.FileMessage) error {
	_, err := c.session.ChannelMessageSendComplex(msg.ChannelID, &discordgo.MessageSend{
		Content: msg.Content,
		Files: []*discordgo.File{
			{Name: msg.Filename, ContentType: "text/plain", Reader: bytes.NewReader(msg.FileBody)},
		},
	})
	return err
}

func (c *Client) RegisterVoiceStateUpdateHandler(handler func(discordpkg.VoiceStateEvent)) {
	c.session.AddHandler(func(s *discordgo.Session, vs *discordgo.VoiceStateUpdate) {
		if vs == nil {
			return
		}
		beforeChannelID := ""
		if vs.BeforeUpdate != nil {
			beforeChannelID = vs.BeforeUpdate.ChannelID
		}
		afterChannelID := vs.ChannelID
		if beforeChannelID == afterChannelID && beforeChannelID != "" {
			return
		}
		if vs.GuildID == "" || vs.UserID == "" {
			return
		}
		handler(discordpkg.VoiceStateEvent{
			GuildID:         vs.GuildID,
			UserID:          vs.UserID,
			UserIsBot:       c.resolveUserIsBot(vs.GuildID, vs.UserID, vs.VoiceState),
			BeforeChannelID: beforeChannelID,
			AfterChannelID:  afterChannelID,
		})
	})
}

func (c *Client) RegisterSlashCommandHandler(handler func(discordpkg.SlashCommandEvent)) {
	c.session.AddHandler(func(s *discordgo.Session, ic *discordgo.InteractionCreate) {
		if ic == nil || ic.Type != discordgo.InteractionApplicationCommand {
			return
		}
		data := ic.ApplicationCommandData()
		if data.Name == "" {
			return
		}
		userID := ""
		if ic.Member != nil && ic.Member.User != nil {
			userID = ic.Member.User.ID
		}
		if userID == "" && ic.User != nil {
			userID = ic.User.ID
		}
		if userID == "" {
			return
		}
		slog.Info("slash command interaction received", "guild_id", ic.GuildID, "channel_id", ic.ChannelID, "command", data.Name, "user_id", userID)
		handler(discordpkg.SlashCommandEvent{
			GuildID:     ic.GuildID,
			ChannelID:   ic.ChannelID,
			CommandName: data.Name,
			UserID:      userID,
			RespondEphemeral: func(content string) error {
				slog.Info("responding to slash interaction", "command", data.Name, "guild_id", ic.GuildID, "channel_id", ic.ChannelID, "user_id", userID)
				return s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: content,
						Flags:   discordgo.MessageFlagsEphemeral,
					},
				})
			},
		})
	})
}

func (c *Client) UpsertGuildSlashCommands(guildID string, defs []discordpkg.SlashCommandDefinition) error {
	appID := c.applicationID()
	if appID == "" {
		return fmt.Errorf("discord application id is not available")
	}
	existing, err := c.session.ApplicationCommands(appID, guildID)
	if err != nil {
		return err
	}
	existingByName := make(map[string]*discordgo.ApplicationCommand, len(existing))
	for _, cmd := range existing {
		if cmd == nil || cmd.Name == "" {
			continue
		}
		existingByName[cmd.Name] = cmd
	}
	for _, def := range defs {
		if err := c.upsertGuildSlashCommand(appID, guildID, def, existingByName); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) upsertGuildSlashCommand(appID, guildID string, def discordpkg.SlashCommandDefinition, existingByName map[string]*discordgo.ApplicationCommand) error {
	if def.Name == "" {
		return nil
	}
	payload := &discordgo.ApplicationCommand{
		Name:        def.Name,
		Description: def.Description,
	}
	cmd, ok := existingByName[def.Name]
	if !ok {
		_, err := c.session.ApplicationCommandCreate(appID, guildID, payload)
		return err
	}
	if cmd.Description == def.Description {
		return nil
	}
	_, err := c.session.ApplicationCommandEdit(appID, guildID, cmd.ID, payload)
	return err
}

func (c *Client) GetUserVoiceChannelID(guildID, userID string) (string, error) {
	if c.session == nil {
		return "", nil
	}
	if c.session.State != nil {
		vs, err := c.session.State.VoiceState(guildID, userID)
		if err == nil && vs != nil {
			return vs.ChannelID, nil
		}
		guild, err := c.session.State.Guild(guildID)
		if err == nil && guild != nil {
			for _, state := range guild.VoiceStates {
				if state != nil && state.UserID == userID {
					return state.ChannelID, nil
				}
			}
		}
	}

	// Cache may be cold right after bot startup; ask Discord API directly as fallback.
	vs, err := c.session.UserVoiceState(guildID, userID)
	if err != nil {
		if isRESTNotFound(err) {
			return "", nil
		}
		return "", err
	}
	if vs == nil {
		return "", nil
	}
	return vs.ChannelID, nil
}

func isRESTNotFound(err error) bool {
	var restErr *discordgo.RESTError
	if !errors.As(err, &restErr) {
		return false
	}
	if restErr.Response == nil {
		return false
	}
	return restErr.Response.StatusCode == http.StatusNotFound
}

func (c *Client) ListVoiceChannelParticipants(guildID, channelID string) ([]discordpkg.VoiceParticipant, error) {
	if c.session == nil || c.session.State == nil {
		return nil, nil
	}
	guild, err := c.session.State.Guild(guildID)
	if err != nil || guild == nil {
		return nil, nil
	}
	participants := make([]discordpkg.VoiceParticipant, 0)
	seen := make(map[string]struct{})
	for _, state := range guild.VoiceStates {
		if state == nil || state.ChannelID != channelID || state.UserID == "" {
			continue
		}
		if _, exists := seen[state.UserID]; exists {
			continue
		}
		seen[state.UserID] = struct{}{}
		participants = append(participants, discordpkg.VoiceParticipant{
			UserID: state.UserID,
			IsBot:  c.resolveUserIsBot(guildID, state.UserID, state),
		})
	}
	return participants, nil
}

func (c *Client) GetBotUserID() (string, error) {
	if c.botUserID != "" {
		return c.botUserID, nil
	}
	if c.session == nil {
		return "", fmt.Errorf("discord session is not initialized")
	}
	if c.session.State != nil && c.session.State.User != nil && c.session.State.User.ID != "" {
		c.botUserID = c.session.State.User.ID
		return c.botUserID, nil
	}
	u, err := c.session.User("@me")
	if err != nil {
		return "", err
	}
	c.botUserID = u.ID
	return c.botUserID, nil
}

func (c *Client) ResolveTranscriptMetadata(ctx context.Context, guildID, channelID string, participantUserIDs []string) (discordpkg.TranscriptMetadata, error) {
	_ = ctx
	meta := discordpkg.TranscriptMetadata{
		DiscordServerID:         guildID,
		DiscordServerName:       guildID,
		DiscordVoiceChannelID:   channelID,
		DiscordVoiceChannelName: channelID,
	}
	if c.session == nil {
		return meta, fmt.Errorf("discord session is not initialized")
	}

	guild := c.resolveGuild(guildID)
	if guild != nil && guild.Name != "" {
		meta.DiscordServerName = guild.Name
	}
	channel := c.resolveChannel(channelID)
	if channel != nil && channel.Name != "" {
		meta.DiscordVoiceChannelName = channel.Name
	}
	if meta.DiscordServerName == guildID {
		slog.Warn("discord guild name could not be resolved; using guild id fallback", "guild_id", guildID)
	}
	if meta.DiscordVoiceChannelName == channelID {
		slog.Warn("discord channel name could not be resolved; using channel id fallback", "channel_id", channelID)
	}

	meta.Participants = c.resolveTranscriptParticipants(guildID, participantUserIDs)
	return meta, nil
}

func (c *Client) resolveUserIsBot(guildID, userID string, state *discordgo.VoiceState) bool {
	if isBot, ok := botFlagFromVoiceState(state); ok {
		return isBot
	}
	if isBot, ok := c.botFlagFromSessionState(guildID, userID); ok {
		return isBot
	}
	return c.botFlagFromUserAPI(userID)
}

func (c *Client) resolveTranscriptParticipants(guildID string, participantUserIDs []string) []discordpkg.TranscriptParticipant {
	seen := make(map[string]struct{}, len(participantUserIDs))
	participants := make([]discordpkg.TranscriptParticipant, 0, len(participantUserIDs))
	for _, userID := range participantUserIDs {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			continue
		}
		if _, exists := seen[userID]; exists {
			continue
		}
		seen[userID] = struct{}{}
		participants = append(participants, c.resolveTranscriptParticipant(guildID, userID))
	}
	return participants
}

func botFlagFromVoiceState(state *discordgo.VoiceState) (bool, bool) {
	if state != nil && state.Member != nil && state.Member.User != nil {
		return state.Member.User.Bot, true
	}
	return false, false
}

func (c *Client) botFlagFromSessionState(guildID, userID string) (bool, bool) {
	if c.session == nil || c.session.State == nil {
		return false, false
	}
	if c.session.State.User != nil && c.session.State.User.ID == userID {
		return true, true
	}
	member, err := c.session.State.Member(guildID, userID)
	if err == nil && member != nil && member.User != nil {
		return member.User.Bot, true
	}
	return false, false
}

func (c *Client) botFlagFromUserAPI(userID string) bool {
	u, err := c.session.User(userID)
	if err != nil {
		return false
	}
	return u.Bot
}

func (c *Client) resolveGuild(guildID string) *discordgo.Guild {
	if c.session == nil {
		return nil
	}
	if c.session.State != nil {
		guild, err := c.session.State.Guild(guildID)
		if err == nil && guild != nil && guild.Name != "" {
			return guild
		}
	}
	guild, err := c.session.Guild(guildID)
	if err != nil || guild == nil {
		return nil
	}
	if guild.Name == "" {
		return nil
	}
	return guild
}

func (c *Client) resolveChannel(channelID string) *discordgo.Channel {
	if c.session == nil {
		return nil
	}
	if c.session.State != nil {
		channel, err := c.session.State.Channel(channelID)
		if err == nil && channel != nil && channel.Name != "" {
			return channel
		}
	}
	channel, err := c.session.Channel(channelID)
	if err != nil || channel == nil {
		return nil
	}
	if channel.Name == "" {
		return nil
	}
	return channel
}

func (c *Client) resolveTranscriptParticipant(guildID, userID string) discordpkg.TranscriptParticipant {
	displayName := userID
	isBot := false

	member := c.resolveGuildMember(guildID, userID)
	if member != nil {
		if member.Nick != "" {
			displayName = member.Nick
		}
		if member.User != nil {
			if displayName == userID {
				displayName = preferredDiscordName(member.User.GlobalName, member.User.Username, userID)
			}
			isBot = member.User.Bot
		}
	}
	if displayName == userID {
		u, err := c.session.User(userID)
		if err == nil && u != nil {
			displayName = preferredDiscordName(u.GlobalName, u.Username, userID)
			isBot = u.Bot
		}
	}

	return discordpkg.TranscriptParticipant{
		UserID:      userID,
		DisplayName: displayName,
		IsBot:       isBot,
	}
}

func (c *Client) resolveGuildMember(guildID, userID string) *discordgo.Member {
	if c.session == nil {
		return nil
	}
	if c.session.State != nil {
		member, err := c.session.State.Member(guildID, userID)
		if err == nil && member != nil {
			return member
		}
	}
	member, err := c.session.GuildMember(guildID, userID)
	if err != nil {
		return nil
	}
	return member
}

func preferredDiscordName(globalName, username, fallback string) string {
	if globalName != "" {
		return globalName
	}
	if username != "" {
		return username
	}
	return fallback
}

func (c *Client) applicationID() string {
	if c.session == nil || c.session.State == nil {
		return ""
	}
	if c.session.State.Application != nil && c.session.State.Application.ID != "" {
		return c.session.State.Application.ID
	}
	if c.session.State.User != nil {
		return c.session.State.User.ID
	}
	return ""
}

func (c *Client) Run() error {
	select {}
}

type voiceConnectionImpl struct {
	vc *discordgo.VoiceConnection
}

func (v *voiceConnectionImpl) Disconnect() error {
	return v.vc.Disconnect()
}

func (v *voiceConnectionImpl) ReceiveAudio(callback func(userID string, opusPCM []byte)) {
	if v.vc.OpusRecv == nil {
		return
	}
	ssrcToUser := make(map[uint32]string)
	var mu sync.RWMutex
	v.vc.AddHandler(func(vc *discordgo.VoiceConnection, vs *discordgo.VoiceSpeakingUpdate) {
		mu.Lock()
		if vs.Speaking {
			ssrcToUser[uint32(vs.SSRC)] = vs.UserID
		}
		mu.Unlock()
	})
	for p := range v.vc.OpusRecv {
		if p == nil || len(p.Opus) == 0 {
			continue
		}
		mu.RLock()
		userID := ssrcToUser[p.SSRC]
		mu.RUnlock()
		if userID == "" {
			userID = strconv.FormatUint(uint64(p.SSRC), 10)
		}
		callback(userID, p.Opus)
	}
}
