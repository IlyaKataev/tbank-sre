package middleware

import (
	"context"
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
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

		lc := &requestLogCtx{}
		r = r.WithContext(context.WithValue(r.Context(), ctxLogCtx, lc))

		rw := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(rw, r)
		status := rw.Status()
		if status == 0 {
			status = http.StatusOK
		}

		// Only operational metadata: never read the body or record credentials,
		// headers or the query string. Logging must not change the request stream.
		evt := log.Info().
			Str("request_id", requestID).
			Str("method", r.Method).
			Str("endpoint", r.URL.Path).
			Int("status_code", status).
			Int64("duration_ms", time.Since(start).Milliseconds()).
			Str("timestamp", start.UTC().Format(time.RFC3339))

		if lc.UserID != uuid.Nil {
			evt = evt.Str("user_id", lc.UserID.String())
		} else {
			evt = evt.Str("user_id", "")
		}

		evt.Msg("request")
	})
}
