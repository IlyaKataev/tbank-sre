package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
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

	cfg, err := config.Load()
	if err != nil {
		log.Error().Err(err).Msg("invalid configuration")
		return 1
	}
	level, _ := zerolog.ParseLevel(cfg.LogLevel) // validated by config.Load
	zerolog.SetGlobalLevel(level)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	connectCtx, cancelConnect := context.WithTimeout(ctx, cfg.DBConnectTimeout)
	pool, err := dbpkg.NewPool(connectCtx, cfg.DBDSN)
	cancelConnect()
	if err != nil {
		log.Error().Err(err).Msg("failed to connect to database")
		return 1
	}
	// Leave time for connection cleanup after the configured HTTP drain.
	defer dbpkg.ClosePool(pool, 2*time.Second)

	var shuttingDown atomic.Bool
	appCfg := app.Config{
		JWTSecret:             cfg.JWTSecret,
		JWTAccessTTL:          cfg.JWTAccessTTL,
		JWTRefreshTTL:         cfg.JWTRefreshTTL,
		OrderRateLimitMinutes: cfg.OrderRateLimitMinutes,
		ReadyTimeout:          cfg.ReadyTimeout,
		IsShuttingDown:        shuttingDown.Load,
	}

	// A SIGTERM lets in-flight database transactions finish during the drain.
	// Request contexts are canceled if the graceful drain expires.
	requestCtx, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           app.NewRouter(pool, appCfg),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return requestCtx },
	}

	listener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		log.Error().Err(err).Msg("failed to listen")
		return 1
	}
	serverErr := make(chan error, 1)
	go func() { serverErr <- srv.Serve(listener) }()
	log.Info().Str("addr", listener.Addr().String()).Msg("server started")

	select {
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("server error")
			return 1
		}
		return 0
	case <-ctx.Done():
	}

	shuttingDown.Store(true)
	stop() // A second interrupt uses the default signal behavior.
	log.Info().Msg("server draining")
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelShutdown()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		cancelRequests()
		_ = srv.Close()
		log.Error().Err(err).Msg("graceful shutdown timed out; active requests canceled")
		return 1
	}
	log.Info().Msg("server shut down")
	return 0
}
