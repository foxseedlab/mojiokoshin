package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
	internalconfig "github.com/foxseedlab/mojiokoshin/internal/config"
)

type envConfig struct {
	Env                        string `env:"ENV" envDefault:"production"`
	DefaultTranscribeLanguage  string `env:"DEFAULT_TRANSCRIBE_LANGUAGE,required"`
	MaxTranscribeDurationMin   int    `env:"MAX_TRANSCRIBE_DURATION_MIN" envDefault:"120"`
	DatabaseURL                string `env:"DATABASE_URL,required"`
	GoogleCloudProjectID       string `env:"GOOGLE_CLOUD_PROJECT_ID,required"`
	GoogleCloudCredentialsJSON string `env:"GOOGLE_CLOUD_CREDENTIALS_JSON,required"`
	GoogleCloudSpeechLocation  string `env:"GOOGLE_CLOUD_SPEECH_LOCATION" envDefault:"asia-northeast1"`
	GoogleCloudSpeechModel     string `env:"GOOGLE_CLOUD_SPEECH_MODEL" envDefault:"chirp_3"`
	DiscordToken               string `env:"DISCORD_TOKEN,required"`
	DiscordGuildID             string `env:"DISCORD_GUILD_ID,required"`
	DiscordAutoTranscribe      bool   `env:"DISCORD_AUTO_TRANSCRIBE" envDefault:"false"`
	DiscordAutoTranscribableVC string `env:"DISCORD_AUTO_TRANSCRIBABLE_VC_ID"`
	DiscordCountOtherBots      bool   `env:"DISCORD_COUNT_OTHER_BOTS_AS_PARTICIPANTS" envDefault:"false"`
	TranscriptTimezone         string `env:"TRANSCRIPT_TIMEZONE" envDefault:"Asia/Tokyo"`
	TranscriptWebhookURL       string `env:"TRANSCRIPT_WEBHOOK_URL"`
}

func Load() (*internalconfig.Config, error) {
	var raw envConfig
	if err := env.Parse(&raw); err != nil {
		return nil, fmt.Errorf("environment variables are invalid or missing: %w", err)
	}

	cfg := &internalconfig.Config{
		Env:                        raw.Env,
		DefaultTranscribeLanguage:  raw.DefaultTranscribeLanguage,
		MaxTranscribeDurationMin:   raw.MaxTranscribeDurationMin,
		DatabaseURL:                raw.DatabaseURL,
		GoogleCloudProjectID:       raw.GoogleCloudProjectID,
		GoogleCloudCredentialsJSON: raw.GoogleCloudCredentialsJSON,
		GoogleCloudSpeechLocation:  raw.GoogleCloudSpeechLocation,
		GoogleCloudSpeechModel:     raw.GoogleCloudSpeechModel,
		DiscordToken:               raw.DiscordToken,
		DiscordGuildID:             raw.DiscordGuildID,
		DiscordAutoTranscribe:      raw.DiscordAutoTranscribe,
		DiscordAutoTranscribableVC: raw.DiscordAutoTranscribableVC,
		DiscordCountOtherBots:      raw.DiscordCountOtherBots,
		TranscriptTimezone:         raw.TranscriptTimezone,
		TranscriptWebhookURL:       raw.TranscriptWebhookURL,
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}
