package config

import (
	"fmt"
	"time"
)

type Config struct {
	Env                        string
	DefaultTranscribeLanguage  string
	MaxTranscribeDurationMin   int
	DatabaseURL                string
	GoogleCloudProjectID       string
	GoogleCloudCredentialsJSON string
	GoogleCloudSpeechLocation  string
	GoogleCloudSpeechModel     string
	DiscordToken               string
	DiscordGuildID             string
	DiscordAutoTranscribe      bool
	DiscordAutoTranscribableVC string
	DiscordCountOtherBots      bool
	TranscriptTimezone         string
	TranscriptWebhookURL       string
	DiscordShowPoweredBy       bool
}

func (c *Config) Validate() error {
	for _, req := range c.requiredFieldChecks() {
		if req.value == "" {
			return fmt.Errorf("%s is required", req.name)
		}
	}
	if c.DiscordAutoTranscribe && c.DiscordAutoTranscribableVC == "" {
		return fmt.Errorf("DISCORD_AUTO_TRANSCRIBABLE_VC_ID is required when DISCORD_AUTO_TRANSCRIBE=true")
	}
	if c.MaxTranscribeDurationMin <= 0 {
		return fmt.Errorf("MAX_TRANSCRIBE_DURATION_MIN must be positive, got %d", c.MaxTranscribeDurationMin)
	}
	if c.TranscriptTimezone == "" {
		return fmt.Errorf("TRANSCRIPT_TIMEZONE is required")
	}
	if _, err := time.LoadLocation(c.TranscriptTimezone); err != nil {
		return fmt.Errorf("TRANSCRIPT_TIMEZONE is invalid: %w", err)
	}
	return nil
}

type requiredEnvField struct {
	name  string
	value string
}

func (c *Config) requiredFieldChecks() []requiredEnvField {
	return []requiredEnvField{
		{name: "DEFAULT_TRANSCRIBE_LANGUAGE", value: c.DefaultTranscribeLanguage},
		{name: "DATABASE_URL", value: c.DatabaseURL},
		{name: "GOOGLE_CLOUD_PROJECT_ID", value: c.GoogleCloudProjectID},
		{name: "GOOGLE_CLOUD_CREDENTIALS_JSON", value: c.GoogleCloudCredentialsJSON},
		{name: "DISCORD_TOKEN", value: c.DiscordToken},
		{name: "DISCORD_GUILD_ID", value: c.DiscordGuildID},
		{name: "TRANSCRIPT_TIMEZONE", value: c.TranscriptTimezone},
	}
}

func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}
