package discord

import (
	"bytes"
	"context"
	"strconv"
	"sync"

	"github.com/bwmarrin/discordgo"
	discordpkg "github.com/foxseedlab/mojiokoshin/internal/discord"
)

type Client struct {
	session *discordgo.Session
	token   string
	guildID string
	vcID    string
}

func NewClient(token, guildID, vcID string) discordpkg.Client {
	return &Client{
		token:   token,
		guildID: guildID,
		vcID:    vcID,
	}
}

func (c *Client) Connect(ctx context.Context) error {
	s, err := discordgo.New("Bot " + c.token)
	if err != nil {
		return err
	}
	c.session = s
	s.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsGuildVoiceStates)
	return s.Open()
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
		if s.State != nil && s.State.User != nil && vs.UserID == s.State.User.ID {
			return
		}
		if vs.GuildID != c.guildID {
			return
		}
		nowInOurChannel := vs.ChannelID == c.vcID
		wasInOurChannel := vs.BeforeUpdate != nil && vs.BeforeUpdate.ChannelID == c.vcID
		if !nowInOurChannel && !wasInOurChannel {
			return
		}
		handler(discordpkg.VoiceStateEvent{
			GuildID:   vs.GuildID,
			ChannelID: vs.ChannelID,
			UserID:    vs.UserID,
			Joined:    nowInOurChannel,
		})
	})
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
