package db

import (
	"context"
	"fmt"
	"time"

	pgxdecimal "github.com/jackc/pgx-shopspring-decimal"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse db dsn: %w", err)
	}

	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		pgxdecimal.Register(conn.TypeMap())
		return nil
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return pool, nil
}

// ClosePool keeps process shutdown bounded even if a handler leaked a connection.
func ClosePool(pool *pgxpool.Pool, timeout time.Duration) {
	closed := make(chan struct{})
	go func() {
		pool.Close()
		close(closed)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-closed:
	case <-timer.C:
		log.Error().Msg("database pool shutdown timed out")
	}
}
