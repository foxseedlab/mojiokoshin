package discord

import "context"

type FileMessage struct {
	ChannelID string
	Content   string
	Filename  string
	FileBody  []byte
}

type SlashCommandDefinition struct {
	Name        string
	Description string
}

type SlashCommandEvent struct {
	GuildID          string
	ChannelID        string
	CommandName      string
	UserID           string
	RespondEphemeral func(content string) error
}

type VoiceStateEvent struct {
	GuildID         string
	UserID          string
	UserIsBot       bool
	BeforeChannelID string
	AfterChannelID  string
}

type VoiceParticipant struct {
	UserID string
	IsBot  bool
}

type TranscriptParticipant struct {
	UserID      string
	DisplayName string
	IsBot       bool
}

type TranscriptMetadata struct {
	DiscordServerID         string
	DiscordServerName       string
	DiscordVoiceChannelID   string
	DiscordVoiceChannelName string
	Participants            []TranscriptParticipant
}

type Client interface {
	Connect(ctx context.Context) error
	Close() error
	JoinVoiceChannel(guildID, channelID string) (VoiceConnection, error)
	SendChannelMessage(channelID, content string) error
	SendChannelMessageWithFile(msg FileMessage) error
	RegisterVoiceStateUpdateHandler(handler func(VoiceStateEvent))
	RegisterSlashCommandHandler(handler func(SlashCommandEvent))
	UpsertGuildSlashCommands(guildID string, defs []SlashCommandDefinition) error
	GetUserVoiceChannelID(guildID, userID string) (string, error)
	ListVoiceChannelParticipants(guildID, channelID string) ([]VoiceParticipant, error)
	GetBotUserID() (string, error)
	ResolveTranscriptMetadata(ctx context.Context, guildID, channelID string, participantUserIDs []string) (TranscriptMetadata, error)
	Run() error
}

type VoiceConnection interface {
	Disconnect() error
	ReceiveAudio(callback func(userID string, opusPCM []byte))
}
