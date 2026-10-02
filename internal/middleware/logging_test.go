package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func captureRequestLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	previousLogger, previousLevel := log.Logger, zerolog.GlobalLevel()
	log.Logger = zerolog.New(&output)
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	t.Cleanup(func() {
		log.Logger = previousLogger
		zerolog.SetGlobalLevel(previousLevel)
	})
	return &output
}

func TestLoggerPreservesLargeBodyAndOmitsSensitiveData(t *testing.T) {
	output := captureRequestLog(t)
	// Deliberately malformed JSON, larger than the previous 64 KiB truncation.
	payload := `{"password":"body-secret","padding":"` + strings.Repeat("x", 128*1024)
	request := httptest.NewRequest(http.MethodPost, "/auth/register?token=query-secret", strings.NewReader(payload))
	request.Header.Set("Authorization", "Bearer authorization-secret")
	request.Header.Set("Cookie", "session=cookie-secret")
	originalBody := request.Body
	userID := uuid.New()

	response := httptest.NewRecorder()
	Logger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != originalBody {
			t.Error("logger replaced the original request stream")
		}
		received, err := io.ReadAll(r.Body)
		if err != nil || string(received) != payload {
			t.Fatalf("request body was changed or truncated: bytes=%d, err=%v", len(received), err)
		}
		setLogUserID(r, userID)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("accepted"))
	})).ServeHTTP(response, request)

	if response.Code != http.StatusCreated || response.Body.String() != "accepted" {
		t.Fatalf("response was changed: %d %s", response.Code, response.Body.String())
	}
	for _, sensitive := range []string{"body-secret", "authorization-secret", "cookie-secret", "query-secret", "request_body"} {
		if strings.Contains(output.String(), sensitive) {
			t.Fatalf("log contains sensitive field %q", sensitive)
		}
	}
	var event map[string]any
	if err := json.Unmarshal(output.Bytes(), &event); err != nil {
		t.Fatalf("request log is not valid JSON: %v", err)
	}
	if event["method"] != http.MethodPost || event["endpoint"] != "/auth/register" ||
		event["status_code"] != float64(http.StatusCreated) || event["user_id"] != userID.String() ||
		event["request_id"] != response.Header().Get("X-Request-Id") || event["request_id"] == "" {
		t.Fatalf("incorrect request metadata: %#v", event)
	}
}

func TestLoggerTracksTheActualResponseStatus(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		handler http.HandlerFunc
	}{
		{"empty response", http.StatusOK, func(http.ResponseWriter, *http.Request) {}},
		{"implicit status", http.StatusOK, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }},
		{"first status wins", http.StatusAccepted, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			w.WriteHeader(http.StatusInternalServerError)
		}},
		{"streaming response", http.StatusOK, func(w http.ResponseWriter, _ *http.Request) {
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("logger removed streaming support")
			}
			flusher.Flush()
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := captureRequestLog(t)
			response := httptest.NewRecorder()
			Logger(test.handler).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/test", nil))
			var event map[string]any
			if err := json.Unmarshal(output.Bytes(), &event); err != nil {
				t.Fatal(err)
			}
			if response.Code != test.status || event["status_code"] != float64(test.status) {
				t.Fatalf("response status=%d, logged status=%v, want=%d", response.Code, event["status_code"], test.status)
			}
		})
	}
}
