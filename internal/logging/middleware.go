package logging

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
)

var sessionIDPattern = regexp.MustCompile(`sessions/(s_[^/]+)`)

// requestMeta holds mutable per-request flags that handlers can set and the
// logging middleware reads back after the handler returns. It is stored as a
// pointer in the request context so mutations in the handler (which may be
// behind wrapping middlewares like http.TimeoutHandler) are visible to the
// logging middleware.
type requestMeta struct {
	chaosInjected bool
}

type requestMetaKey struct{}

// MarkChaosInjected flags the current request as a chaos-injected failure so
// the logging middleware logs it at Info level instead of Error/Warn.
// Must be called with the http.Request from the handler.
func MarkChaosInjected(r *http.Request) {
	if meta, ok := r.Context().Value(requestMetaKey{}).(*requestMeta); ok {
		meta.chaosInjected = true
	}
}

// HTTPMiddleware returns HTTP middleware that creates a request-scoped logger
// with trace, request ID, and other contextual fields, and logs request
// completion with status code and duration.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Extract trace and span from X-Cloud-Trace-Context header
		// Format: TRACE_ID/SPAN_ID;o=TRACE_TRUE
		var traceID, spanID string
		if traceHeader := r.Header.Get("X-Cloud-Trace-Context"); traceHeader != "" {
			parts := strings.SplitN(traceHeader, "/", 2)
			if len(parts) > 0 {
				traceID = parts[0]
			}
			if len(parts) > 1 {
				spanPart := parts[1]
				if idx := strings.Index(spanPart, ";"); idx != -1 {
					spanID = spanPart[:idx]
				} else {
					spanID = spanPart
				}
			}
		}

		// Generate request ID (first 8 chars of UUID)
		requestID := uuid.New().String()[:8]

		// Build logger fields
		attrs := []any{
			FieldRequestID, requestID,
			FieldMethod, r.Method,
			FieldPath, r.URL.Path,
			FieldRemoteIP, r.RemoteAddr,
		}

		// Add trace fields if available
		if traceID != "" {
			projectID := GetProjectID()
			if projectID != "" {
				attrs = append(attrs, FieldTraceID, fmt.Sprintf("projects/%s/traces/%s", projectID, traceID))
			} else {
				attrs = append(attrs, FieldTraceID, traceID)
			}
		}
		if spanID != "" {
			attrs = append(attrs, FieldSpanID, spanID)
		}

		// Extract X-Team-Id header
		if teamID := r.Header.Get("X-Team-Id"); teamID != "" {
			attrs = append(attrs, FieldTeamID, teamID)
		}

		// Extract sessionId from URL path
		if matches := sessionIDPattern.FindStringSubmatch(r.URL.Path); len(matches) > 1 {
			attrs = append(attrs, FieldSessionID, matches[1])
		}

		// Create request-scoped logger and inject into context
		logger := slog.Default().With(attrs...)
		ctx := WithLogger(r.Context(), logger)
		meta := &requestMeta{}
		ctx = context.WithValue(ctx, requestMetaKey{}, meta)
		r = r.WithContext(ctx)

		// Wrap response writer to capture status code and bytes
		rw := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		// Panic recovery
		defer func() {
			if rec := recover(); rec != nil {
				stack := string(debug.Stack())
				logger.Error("request panicked",
					"panic", fmt.Sprintf("%v", rec),
					"stack", stack,
					FieldStatusCode, http.StatusInternalServerError,
					FieldDuration, float64(time.Since(start).Milliseconds()),
					"responseSize", 0,
				)
				http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(rw, r)

		// Log request completion
		duration := float64(time.Since(start).Milliseconds())
		logAttrs := []any{
			FieldStatusCode, rw.statusCode,
			FieldDuration, duration,
			"responseSize", rw.bytes,
		}

		switch {
		case meta.chaosInjected:
			logAttrs = append(logAttrs, "chaos", true)
			logger.Info("request completed", logAttrs...)
		case rw.statusCode >= 500:
			logger.Error("request completed", logAttrs...)
		case rw.statusCode >= 400:
			logger.Warn("request completed", logAttrs...)
		default:
			logger.Info("request completed", logAttrs...)
		}
	})
}

// responseWriter wraps http.ResponseWriter to capture the status code and bytes written.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	bytes      int
	written    bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.statusCode = http.StatusOK
		rw.written = true
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytes += n
	return n, err
}
