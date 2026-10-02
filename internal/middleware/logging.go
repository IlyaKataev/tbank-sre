package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const ctxLogCtx contextKey = "log_ctx"

// requestLogCtx is a mutable holder placed in context by Logger so that
// downstream middleware can back-fill the authenticated user_id.
type requestLogCtx struct {
	UserID uuid.UUID
}

// setLogUserID writes the authenticated user id into the shared log context
// so that Logger can include it after the handler chain finishes.
func setLogUserID(r *http.Request, id uuid.UUID) {
	if lc, ok := r.Context().Value(ctxLogCtx).(*requestLogCtx); ok {
		lc.UserID = id
	}
}

func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := uuid.New().String()

		w.Header().Set("X-Request-Id", requestID)

		var bodyStr string
		if isMutating(r.Method) && r.Body != nil && r.ContentLength > 0 {
			bodyBytes, _ := io.ReadAll(io.LimitReader(r.Body, 64*1024))
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			bodyStr = maskSensitive(bodyBytes)
		}

		lc := &requestLogCtx{}
		r = r.WithContext(context.WithValue(r.Context(), ctxLogCtx, lc))

		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)

		evt := log.Info().
			Str("request_id", requestID).
			Str("method", r.Method).
			Str("endpoint", r.URL.Path).
			Int("status_code", rw.statusCode).
			Int64("duration_ms", time.Since(start).Milliseconds()).
			Str("timestamp", start.UTC().Format(time.RFC3339))

		if lc.UserID != uuid.Nil {
			evt = evt.Str("user_id", lc.UserID.String())
		} else {
			evt = evt.Str("user_id", "")
		}

		if bodyStr != "" {
			evt = evt.RawJSON("request_body", json.RawMessage(bodyStr))
		}

		evt.Msg("request")
		_ = zerolog.GlobalLevel()
	})
}

func isMutating(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete
}

func maskSensitive(body []byte) string {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return string(body)
	}
	for k := range m {
		if strings.Contains(strings.ToLower(k), "password") || strings.Contains(strings.ToLower(k), "token") {
			m[k] = "***"
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
