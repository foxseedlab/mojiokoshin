package config

import "testing"

func TestValidate_Valid(t *testing.T) {
	cfg := &Config{
		Env:                        "development",
		DefaultTranscribeLanguage:  "ja-JP",
		MaxTranscribeDurationMin:   30,
		DatabaseURL:                "postgres://user:pass@localhost:5432/mojiokoshin",
		GoogleCloudProjectID:       "project-id",
		GoogleCloudCredentialsJSON: `{"type":"service_account"}`,
		DiscordToken:               "token",
		DiscordGuildID:             "guild",
		DiscordVCID:                "vc",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidate_InvalidMaxDuration(t *testing.T) {
	cfg := &Config{
		DefaultTranscribeLanguage:  "ja-JP",
		MaxTranscribeDurationMin:   0,
		DatabaseURL:                "postgres://user:pass@localhost:5432/mojiokoshin",
		GoogleCloudProjectID:       "project-id",
		GoogleCloudCredentialsJSON: `{"type":"service_account"}`,
		DiscordToken:               "token",
		DiscordGuildID:             "guild",
		DiscordVCID:                "vc",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for non-positive max duration")
	}
}

func TestValidate_MissingRequired(t *testing.T) {
	cfg := &Config{}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error when required fields are missing")
	}
}

func TestIsDevelopment(t *testing.T) {
	cfg := &Config{Env: "development"}
	if !cfg.IsDevelopment() {
		t.Fatal("expected development mode")
	}
	cfg.Env = "production"
	if cfg.IsDevelopment() {
		t.Fatal("expected non-development mode")
	}
}
