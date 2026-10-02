package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func validEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"DB_DSN", "JWT_SECRET", "HTTP_PORT", "LOG_LEVEL", "JWT_ACCESS_TTL", "JWT_REFRESH_TTL",
		"ORDER_RATE_LIMIT_MINUTES", "DB_CONNECT_TIMEOUT", "SHUTDOWN_TIMEOUT", "READY_TIMEOUT",
	} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DB_DSN", "postgres://user:password@db:5432/marketplace?sslmode=disable")
	t.Setenv("JWT_SECRET", "a-test-secret-with-at-least-32-bytes")
}

func TestLoadDefaults(t *testing.T) {
	validEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPPort != "8080" || cfg.LogLevel != "info" || cfg.JWTAccessTTL != 15*time.Minute ||
		cfg.JWTRefreshTTL != 168*time.Hour || cfg.OrderRateLimitMinutes != 1 ||
		cfg.DBConnectTimeout != 10*time.Second || cfg.ShutdownTimeout != 10*time.Second || cfg.ReadyTimeout != 2*time.Second {
		t.Fatal("unexpected configuration defaults")
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	for _, test := range []struct{ key, value string }{
		{"DB_DSN", ""},
		{"DB_DSN", "postgres://user:do-not-expose-this@host:invalid/database"},
		{"JWT_SECRET", ""},
		{"JWT_SECRET", "too-short"},
		{"HTTP_PORT", "0"},
		{"HTTP_PORT", "65536"},
		{"HTTP_PORT", "eight"},
		{"HTTP_PORT", ""},
		{"LOG_LEVEL", "loud"},
		{"JWT_ACCESS_TTL", "15minutes"},
		{"JWT_REFRESH_TTL", "-1s"},
		{"DB_CONNECT_TIMEOUT", "0s"},
		{"SHUTDOWN_TIMEOUT", "invalid"},
		{"READY_TIMEOUT", ""},
		{"ORDER_RATE_LIMIT_MINUTES", "-1"},
		{"ORDER_RATE_LIMIT_MINUTES", "invalid"},
	} {
		t.Run(test.key+"/"+test.value, func(t *testing.T) {
			validEnvironment(t)
			t.Setenv(test.key, test.value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), test.key) {
				t.Fatalf("expected a %s validation error, got %v", test.key, err)
			}
			if strings.Contains(err.Error(), "do-not-expose-this") {
				t.Fatal("validation error exposed the database password")
			}
		})
	}
}

func TestLoadOverrides(t *testing.T) {
	validEnvironment(t)
	t.Setenv("HTTP_PORT", "9000")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("JWT_ACCESS_TTL", "5m")
	t.Setenv("ORDER_RATE_LIMIT_MINUTES", "0")
	t.Setenv("READY_TIMEOUT", "250ms")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPPort != "9000" || cfg.LogLevel != "debug" || cfg.JWTAccessTTL != 5*time.Minute ||
		cfg.OrderRateLimitMinutes != 0 || cfg.ReadyTimeout != 250*time.Millisecond {
		t.Fatal("environment overrides were not applied")
	}
}
