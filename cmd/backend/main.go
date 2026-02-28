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
	runBot(cfg, injector)
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

func runBot(cfg *config.Config, injector do.Injector) {
	dc, manager := resolveRuntime(injector)
	defer recoverBotPanic(dc, manager)

	connectDiscordOrExit(dc)
	configureSlashAndHandlersOrExit(cfg, dc, manager)

	done := startDiscordRunLoop(dc)
	waitForShutdown(done)
	shutdownAllSessions(manager, session.StopReasonServerClosed)
	closeDiscord(dc)
}

func resolveRuntime(injector do.Injector) (discordpkg.Client, *session.Manager) {
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
	return dc, manager
}

func recoverBotPanic(dc discordpkg.Client, manager *session.Manager) {
	recovered := recover()
	if recovered == nil {
		return
	}
	slog.Error("panic recovered in bot runtime", "panic", recovered)
	shutdownAllSessions(manager, session.StopReasonUnknownError)
	closeDiscord(dc)
	os.Exit(1)
}

func connectDiscordOrExit(dc discordpkg.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), discordConnectTimeout)
	defer cancel()

	slog.Info("startup: connecting to discord gateway")
	if err := dc.Connect(ctx); err != nil {
		slog.Error("discord connect failed", "error", err)
		os.Exit(1)
	}
	slog.Info("startup: discord connected")
}

func configureSlashAndHandlersOrExit(cfg *config.Config, dc discordpkg.Client, manager *session.Manager) {
	botUserID, err := dc.GetBotUserID()
	if err != nil {
		slog.Error("failed to resolve bot user id", "error", err)
		os.Exit(1)
	}
	manager.SetBotUserID(botUserID)

	if err := dc.UpsertGuildSlashCommands(cfg.DiscordGuildID, session.SlashCommandDefinitions()); err != nil {
		slog.Error("failed to upsert slash commands", "error", err, "guild_id", cfg.DiscordGuildID)
		os.Exit(1)
	}

	dc.RegisterVoiceStateUpdateHandler(manager.HandleVoiceStateUpdate)
	dc.RegisterSlashCommandHandler(manager.HandleSlashCommand)
	slog.Info("discord handlers registered", "guild_id", cfg.DiscordGuildID, "commands", []string{"mojiokoshi", "mojiokoshi-stop"})
}

func startDiscordRunLoop(dc discordpkg.Client) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		slog.Info("startup: entering discord run loop")
		if err := dc.Run(); err != nil {
			slog.Error("discord run failed", "error", err)
		}
		close(done)
	}()
	return done
}

func waitForShutdown(done <-chan struct{}) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case <-sigCh:
		slog.Info("shutdown signal received")
	case <-done:
		slog.Info("discord run loop ended")
	}
}

func shutdownAllSessions(manager *session.Manager, reason string) {
	if manager == nil {
		return
	}
	stopped := manager.StopAllSessions(reason)
	slog.Info("sessions stopped", "count", stopped, "reason", reason)
}

func closeDiscord(dc discordpkg.Client) {
	if dc == nil {
		return
	}
	if err := dc.Close(); err != nil {
		slog.Error("discord close failed", "error", err)
	}
}
