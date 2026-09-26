package platform

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/nats-io/nats.go"
)

func NATSURL() string {
	if value := os.Getenv("NATS_URL"); value != "" {
		return value
	}
	return nats.DefaultURL
}

func Connect(name string) (*nats.Conn, error) {
	return nats.Connect(NATSURL(), nats.Name(name), nats.MaxReconnects(-1), nats.ReconnectWait(time.Second))
}

func NewID() string {
	bytes := make([]byte, 10)
	if _, err := rand.Read(bytes); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(bytes)
}

func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// Error writes a client-safe error envelope. The exact cause is always logged
// with the request ID; it is only exposed to clients when explicitly enabled
// for a local development stack.
func Error(log *slog.Logger, w http.ResponseWriter, r *http.Request, status int, code, message string, cause error) {
	requestID := RequestID(r)
	attributes := []any{"request_id", requestID, "code", code, "status", status, "method", r.Method, "path", r.URL.Path}
	if cause != nil {
		attributes = append(attributes, "error", cause)
		log.Error("request failed", attributes...)
	} else {
		log.Warn("request rejected", attributes...)
	}
	response := map[string]string{"error": message, "code": code, "requestId": requestID}
	if cause != nil && os.Getenv("EXPOSE_TECHNICAL_ERRORS") == "true" {
		response["technicalMessage"] = cause.Error()
	}
	JSON(w, status, response)
}

func RequestID(r *http.Request) string {
	if requestID := r.Header.Get("X-Request-Id"); requestID != "" {
		return requestID
	}
	return NewID()
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (w *responseRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

// HTTP adds a request ID to every response and logs non-success HTTP traffic
// across each service consistently.
func HTTP(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := RequestID(r)
		r.Header.Set("X-Request-Id", requestID)
		w.Header().Set("X-Request-Id", requestID)
		recorder := &responseRecorder{ResponseWriter: w}
		started := time.Now()
		next.ServeHTTP(recorder, r)
		if recorder.status >= 400 {
			log.Warn("http request completed with error", "request_id", requestID, "method", r.Method, "path", r.URL.Path, "status", recorder.status, "duration_ms", time.Since(started).Milliseconds())
		}
	})
}

func DecodeJSON(r *http.Request, value any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(value)
}

func Logger(service string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{})).With("service", service)
}
