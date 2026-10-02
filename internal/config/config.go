package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

type Config struct {
	DBDSN                 string
	JWTSecret             string
	JWTAccessTTL          time.Duration
	JWTRefreshTTL         time.Duration
	OrderRateLimitMinutes int
	HTTPPort              string
	LogLevel              string
	DBConnectTimeout      time.Duration
	ShutdownTimeout       time.Duration
	ReadyTimeout          time.Duration
}

// Load validates configuration before any network connection is attempted.
// Error messages deliberately omit values that may contain credentials.
func Load() (Config, error) {
	cfg := Config{
		DBDSN:     os.Getenv("DB_DSN"),
		JWTSecret: os.Getenv("JWT_SECRET"),
		HTTPPort:  getEnv("HTTP_PORT", "8080"),
		LogLevel:  getEnv("LOG_LEVEL", "info"),
	}
	if strings.TrimSpace(cfg.DBDSN) == "" {
		return Config{}, errors.New("DB_DSN is required")
	}
	if _, err := pgxpool.ParseConfig(cfg.DBDSN); err != nil {
		return Config{}, errors.New("DB_DSN must be a valid PostgreSQL connection string")
	}
	if len(strings.TrimSpace(cfg.JWTSecret)) < 32 {
		return Config{}, errors.New("JWT_SECRET must contain at least 32 bytes")
	}
	port, err := strconv.Atoi(cfg.HTTPPort)
	if err != nil || port < 1 || port > 65535 {
		return Config{}, errors.New("HTTP_PORT must be an integer between 1 and 65535")
	}
	if _, err := zerolog.ParseLevel(cfg.LogLevel); err != nil {
		return Config{}, errors.New("LOG_LEVEL must be a valid zerolog level (for example info or debug)")
	}
	for _, field := range []struct {
		key      string
		fallback time.Duration
		target   *time.Duration
	}{
		{"JWT_ACCESS_TTL", 15 * time.Minute, &cfg.JWTAccessTTL},
		{"JWT_REFRESH_TTL", 168 * time.Hour, &cfg.JWTRefreshTTL},
		{"DB_CONNECT_TIMEOUT", 10 * time.Second, &cfg.DBConnectTimeout},
		{"SHUTDOWN_TIMEOUT", 10 * time.Second, &cfg.ShutdownTimeout},
		{"READY_TIMEOUT", 2 * time.Second, &cfg.ReadyTimeout},
	} {
		value, err := time.ParseDuration(getEnv(field.key, field.fallback.String()))
		if err != nil || value <= 0 {
			return Config{}, fmt.Errorf("%s must be a positive duration, for example 10s", field.key)
		}
		*field.target = value
	}
	cfg.OrderRateLimitMinutes, err = strconv.Atoi(getEnv("ORDER_RATE_LIMIT_MINUTES", "1"))
	if err != nil || cfg.OrderRateLimitMinutes < 0 || cfg.OrderRateLimitMinutes > 525600 {
		return Config{}, errors.New("ORDER_RATE_LIMIT_MINUTES must be an integer between 0 and 525600")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}
