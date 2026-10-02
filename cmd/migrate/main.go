package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"marketplace/internal/app"
	"marketplace/internal/config"
	dbpkg "marketplace/internal/db"
)

func main() {
	os.Exit(run())
}

func run() int {
	zerolog.TimeFieldFormat = time.RFC3339
	log.Logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
	if len(os.Args) > 2 || (len(os.Args) == 2 && os.Args[1] != "up") {
		log.Error().Msg("usage: migrate [up]")
		return 1
	}
	cfg, err := config.Load()
	if err != nil {
		log.Error().Err(err).Msg("invalid configuration")
		return 1
	}
	level, _ := zerolog.ParseLevel(cfg.LogLevel)
	zerolog.SetGlobalLevel(level)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	connectCtx, cancel := context.WithTimeout(ctx, cfg.DBConnectTimeout)
	pool, err := dbpkg.NewPool(connectCtx, cfg.DBDSN)
	cancel()
	if err != nil {
		log.Error().Err(err).Msg("failed to connect to database")
		return 1
	}
	defer dbpkg.ClosePool(pool, 2*time.Second)

	migrationErr := make(chan error, 1)
	go func() { migrationErr <- app.RunMigrations(pool) }()
	select {
	case err := <-migrationErr:
		if err != nil {
			log.Error().Err(err).Msg("failed to run migrations")
			return 1
		}
	case <-ctx.Done():
		stop()
		log.Error().Msg("migration interrupted")
		return 1
	}
	log.Info().Msg("migrations applied successfully")
	return 0
}
