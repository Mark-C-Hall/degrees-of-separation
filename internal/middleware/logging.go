package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/trace"
)

type contextKey string

const RequestIDKey contextKey = "request_id"

type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusResponseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Logging records one structured log line per request, including trace_id and
// span_id when a span is present for log-trace correlation.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := newRequestID()
			ctx := context.WithValue(r.Context(), RequestIDKey, id)
			r = r.WithContext(ctx)

			wrapped := &statusResponseWriter{ResponseWriter: w, status: 200}
			start := time.Now()

			next.ServeHTTP(wrapped, r)

			args := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", wrapped.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", id,
				"remote_addr", r.RemoteAddr,
			}

			// otelhttp (inner middleware) has already created the span by the
			// time we log, so it is available in context on the way out.
			span := trace.SpanFromContext(ctx)
			sc := span.SpanContext()
			if sc.IsValid() {
				args = append(args,
					"trace_id", sc.TraceID().String(),
					"span_id", sc.SpanID().String(),
				)
			}

			logger.InfoContext(ctx, "request", args...)
		})
	}
}

func newRequestID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
