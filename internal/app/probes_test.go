package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReadiness(t *testing.T) {
	for _, test := range []struct {
		name       string
		pingError  error
		draining   bool
		wantStatus int
		wantBody   string
	}{
		{"healthy database", nil, false, http.StatusOK, "ready"},
		{"database unavailable", errors.New("sensitive connection details"), false, http.StatusServiceUnavailable, "unavailable"},
		{"draining", nil, true, http.StatusServiceUnavailable, "draining"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			ping := func(ctx context.Context) error {
				calls++
				if _, ok := ctx.Deadline(); !ok {
					t.Error("database ping must have a deadline")
				}
				return test.pingError
			}
			response := httptest.NewRecorder()
			readyHandler(ping, time.Second, func() bool { return test.draining })(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			if response.Code != test.wantStatus || !strings.Contains(response.Body.String(), test.wantBody) {
				t.Fatalf("unexpected probe response: %d %s", response.Code, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "sensitive") {
				t.Fatal("readiness must not cache its result or expose database errors")
			}
			if test.draining && calls != 0 {
				t.Fatal("draining process should not query the database")
			}
		})
	}
}

func TestReadinessCancelsSlowPing(t *testing.T) {
	ping := func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}
	response := httptest.NewRecorder()
	readyHandler(ping, time.Millisecond, nil)(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 after ping timeout, got %d", response.Code)
	}
}

func TestLivenessDoesNotNeedDatabaseOrAuthentication(t *testing.T) {
	router := NewRouter(nil, Config{JWTSecret: "test-secret"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "ok") {
		t.Fatalf("unexpected liveness: %d %s", response.Code, response.Body.String())
	}
}
