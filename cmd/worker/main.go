package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"dist-command-exec/internal/config"
	"dist-command-exec/internal/worker"
	"dist-command-exec/pkg/executor"
)

func main() {
	// Load config
	cfg, err := config.LoadWorker()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	// Setup logging
	setupLogging(cfg.LogLevel)

	log.Info().
		Str("worker_id", cfg.ID).
		Str("address", cfg.Address).
		Str("master", cfg.MasterAddress).
		Msg("starting worker node")

	// Build executor registry from configuration
	registry := executor.NewRegistry()
	if cfg.Executors.Command.Enabled {
		cmdExec := executor.NewCommandExecutorWithLimits(
			cfg.Executors.Command.AllowedCommandsList(),
			cfg.Executors.Command.BlockedCommandsList(),
		)
		registry.Register(cmdExec)
		log.Debug().
			Strs("allowed", cfg.Executors.Command.AllowedCommandsList()).
			Strs("blocked", cfg.Executors.Command.BlockedCommandsList()).
			Msg("registered command executor")
	}

	// Create server
	srv, err := worker.NewServer(cfg, registry)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create server")
	}

	// Graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Run server
	if err := srv.Run(ctx); err != nil {
		log.Fatal().Err(err).Msg("server error")
	}

	log.Info().Msg("worker node stopped")
}

func setupLogging(level string) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	// Pretty logging for development
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Set log level
	switch level {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}
