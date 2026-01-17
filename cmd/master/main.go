package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"dist-command-exec/internal/config"
	"dist-command-exec/internal/master"
)

func main() {
	// Load config
	cfg, err := config.LoadMaster()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	// Setup logging
	setupLogging(cfg.LogLevel)

	log.Info().
		Str("address", cfg.Address).
		Int("max_jobs", cfg.MaxConcurrentJobs).
		Msg("starting master node")

	// Create server
	srv, err := master.NewServer(cfg)
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

	log.Info().Msg("master node stopped")
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
