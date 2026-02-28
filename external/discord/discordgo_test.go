package discord

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestSession(t *testing.T, rt roundTripFunc) *discordgo.Session {
	t.Helper()
	s, err := discordgo.New("Bot test-token")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	if rt != nil {
		s.Client = &http.Client{Transport: rt}
	}
	return s
}

func TestGetUserVoiceChannelID_UsesStateCacheFirst(t *testing.T) {
	s := newTestSession(t, func(req *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected REST call: %s %s", req.Method, req.URL.String())
		return nil, nil
	})
	if err := s.State.GuildAdd(&discordgo.Guild{
		ID: "guild-1",
		VoiceStates: []*discordgo.VoiceState{
			{GuildID: "guild-1", ChannelID: "vc-1", UserID: "user-1"},
		},
	}); err != nil {
		t.Fatalf("failed to add guild to state: %v", err)
	}

	c := &Client{session: s}
	channelID, err := c.GetUserVoiceChannelID("guild-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if channelID != "vc-1" {
		t.Fatalf("expected vc-1, got %q", channelID)
	}
}

func TestGetUserVoiceChannelID_FallsBackToRESTWhenStateIsCold(t *testing.T) {
	s := newTestSession(t, func(req *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(req.URL.Path, "/guilds/guild-1/voice-states/user-1") {
			t.Fatalf("unexpected request path: %s", req.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body: io.NopCloser(strings.NewReader(
				`{"guild_id":"guild-1","channel_id":"vc-rest","user_id":"user-1","session_id":"x","deaf":false,"mute":false,"self_deaf":false,"self_mute":false,"self_video":false,"suppress":false}`,
			)),
			Header: make(http.Header),
		}, nil
	})

	c := &Client{session: s}
	channelID, err := c.GetUserVoiceChannelID("guild-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if channelID != "vc-rest" {
		t.Fatalf("expected vc-rest, got %q", channelID)
	}
}

func TestGetUserVoiceChannelID_ReturnsEmptyOnRESTNotFound(t *testing.T) {
	s := newTestSession(t, func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     "404 Not Found",
			Body:       io.NopCloser(strings.NewReader(`{"message":"Unknown Voice State","code":10065}`)),
			Header:     make(http.Header),
		}, nil
	})

	c := &Client{session: s}
	channelID, err := c.GetUserVoiceChannelID("guild-1", "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if channelID != "" {
		t.Fatalf("expected empty channel id, got %q", channelID)
	}
}
