package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	DBDSN                 string
	JWTSecret             string
	JWTAccessTTL          time.Duration
	JWTRefreshTTL         time.Duration
	OrderRateLimitMinutes int
	HTTPPort              string
	LogLevel              string
}

func Load() Config {
	return Config{
		DBDSN:                 getEnv("DB_DSN", "postgres://marketplace:marketplace@localhost:5432/marketplace?sslmode=disable"),
		JWTSecret:             getEnv("JWT_SECRET", "dev-secret-change-in-production"),
		JWTAccessTTL:          getDuration("JWT_ACCESS_TTL", 15*time.Minute),
		JWTRefreshTTL:         getDuration("JWT_REFRESH_TTL", 168*time.Hour),
		OrderRateLimitMinutes: getInt("ORDER_RATE_LIMIT_MINUTES", 1),
		HTTPPort:              getEnv("HTTP_PORT", "8080"),
		LogLevel:              getEnv("LOG_LEVEL", "info"),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func getInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
