package main

import (
	"context"
	"os"

	"github.com/rs/zerolog/log"

	"marketplace/internal/app"
	"marketplace/internal/config"
	dbpkg "marketplace/internal/db"
)

func main() {
	cfg := config.Load()

	ctx := context.Background()

	pool, err := dbpkg.NewPool(ctx, cfg.DBDSN)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer pool.Close()

	if err := app.RunMigrations(pool); err != nil {
		log.Fatal().Err(err).Msg("failed to run migrations")
	}
	log.Info().Msg("migrations applied successfully")
	os.Exit(0)
}
