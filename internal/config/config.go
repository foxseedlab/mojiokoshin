package config

import "fmt"

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
	DiscordVCID                string
	TranscriptWebhookURL       string
}

func (c *Config) Validate() error {
	if c.DefaultTranscribeLanguage == "" {
		return fmt.Errorf("DEFAULT_TRANSCRIBE_LANGUAGE is required")
	}
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if c.GoogleCloudProjectID == "" {
		return fmt.Errorf("GOOGLE_CLOUD_PROJECT_ID is required")
	}
	if c.GoogleCloudCredentialsJSON == "" {
		return fmt.Errorf("GOOGLE_CLOUD_CREDENTIALS_JSON is required")
	}
	if c.DiscordToken == "" {
		return fmt.Errorf("DISCORD_TOKEN is required")
	}
	if c.DiscordGuildID == "" {
		return fmt.Errorf("DISCORD_TRANSCRIBABLE_GUILD_ID is required")
	}
	if c.DiscordVCID == "" {
		return fmt.Errorf("DISCORD_TRANSCRIBABLE_VC_ID is required")
	}
	if c.MaxTranscribeDurationMin <= 0 {
		return fmt.Errorf("MAX_TRANSCRIBE_DURATION_MIN must be positive, got %d", c.MaxTranscribeDurationMin)
	}
	return nil
}

func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}
