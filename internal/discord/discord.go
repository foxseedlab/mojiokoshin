package discord

import "context"

type FileMessage struct {
	ChannelID string
	Content   string
	Filename  string
	FileBody  []byte
}

type VoiceStateEvent struct {
	GuildID   string
	ChannelID string
	UserID    string
	Joined    bool
}

type Client interface {
	Connect(ctx context.Context) error
	Close() error
	JoinVoiceChannel(guildID, channelID string) (VoiceConnection, error)
	SendChannelMessage(channelID, content string) error
	SendChannelMessageWithFile(msg FileMessage) error
	RegisterVoiceStateUpdateHandler(handler func(VoiceStateEvent))
	Run() error
}

type VoiceConnection interface {
	Disconnect() error
	ReceiveAudio(callback func(userID string, opusPCM []byte))
}
