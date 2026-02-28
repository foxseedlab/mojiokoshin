package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	audioimpl "github.com/foxseedlab/mojiokoshin/external/audio"
	configloader "github.com/foxseedlab/mojiokoshin/external/config"
	"github.com/foxseedlab/mojiokoshin/external/discord"
	repositoryimpl "github.com/foxseedlab/mojiokoshin/external/repository"
	transcriberimpl "github.com/foxseedlab/mojiokoshin/external/transcriber"
	webhookimpl "github.com/foxseedlab/mojiokoshin/external/webhook"
	"github.com/foxseedlab/mojiokoshin/internal/config"
	discordpkg "github.com/foxseedlab/mojiokoshin/internal/discord"
	"github.com/foxseedlab/mojiokoshin/internal/session"
	"github.com/samber/do/v2"
)

const discordConnectTimeout = 20 * time.Second

func main() {
	slog.Info("startup: loading configuration")
	cfg := mustLoadConfig()
	initLogger(cfg)
	slog.Info("startup: configuration loaded", "env", cfg.Env)

	slog.Info("startup: building dependency graph")
	injector := setupDI(cfg)

	slog.Info("startup: launching discord bot")
	runBot(injector)
}

func mustLoadConfig() *config.Config {
	cfg, err := configloader.Load()
	if err != nil {
		slog.Error("config validation failed", "error", err)
		os.Exit(1)
	}
	return cfg
}

func initLogger(cfg *config.Config) {
	logLevel := slog.LevelInfo
	if cfg.IsDevelopment() {
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})))
}

func setupDI(cfg *config.Config) do.Injector {
	injector := do.New()

	do.ProvideValue(injector, cfg)
	repositoryimpl.RegisterDI(injector)
	audioimpl.RegisterDI(injector)
	discord.RegisterDI(injector)
	transcriberimpl.RegisterDI(injector)
	webhookimpl.RegisterDI(injector)
	session.RegisterDI(injector)

	return injector
}

func runBot(injector do.Injector) {
	dc, err := do.Invoke[discordpkg.Client](injector)
	if err != nil {
		slog.Error("failed to resolve discord client", "error", err)
		os.Exit(1)
	}
	manager, err := do.Invoke[*session.Manager](injector)
	if err != nil {
		slog.Error("failed to resolve session manager", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), discordConnectTimeout)
	defer cancel()

	slog.Info("startup: connecting to discord gateway")
	if err := dc.Connect(ctx); err != nil {
		slog.Error("discord connect failed", "error", err)
		os.Exit(1)
	}
	slog.Info("startup: discord connected")

	dc.RegisterVoiceStateUpdateHandler(manager.HandleVoiceStateUpdate)
	defer func() {
		if err := dc.Close(); err != nil {
			slog.Error("discord close failed", "error", err)
		}
	}()

	done := make(chan struct{})
	go func() {
		slog.Info("startup: entering discord run loop")
		if err := dc.Run(); err != nil {
			slog.Error("discord run failed", "error", err)
		}
		close(done)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sigCh:
		slog.Info("shutting down")
	case <-done:
	}
}
