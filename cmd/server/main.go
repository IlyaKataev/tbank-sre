package main

import (
	"context"
	"net/http"
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
	cfg := config.Load()

	zerolog.TimeFieldFormat = time.RFC3339
	log.Logger = zerolog.New(os.Stdout).With().Timestamp().Logger()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := dbpkg.NewPool(ctx, cfg.DBDSN)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer pool.Close()

	if err := app.RunMigrations(pool); err != nil {
		log.Fatal().Err(err).Msg("failed to run migrations")
	}
	log.Info().Msg("migrations applied")

	appCfg := app.Config{
		JWTSecret:             cfg.JWTSecret,
		JWTAccessTTL:          cfg.JWTAccessTTL,
		JWTRefreshTTL:         cfg.JWTRefreshTTL,
		OrderRateLimitMinutes: cfg.OrderRateLimitMinutes,
	}

	srv := &http.Server{
		Addr:         ":" + cfg.HTTPPort,
		Handler:      app.NewRouter(pool, appCfg),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		log.Info().Str("addr", srv.Addr).Msg("server started")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server error")
		}
	}()

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Info().Msg("server shut down")
}
