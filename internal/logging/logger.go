package logging

import (
	"log/slog"
	"os"
	"strings"
)

// gcpProjectID stores the GCP project ID for trace URL formatting.
var gcpProjectID string

// Init initializes the default slog logger with a JSON handler configured
// for GCP Cloud Run structured logging.
func Init(level string, projectID string) {
	gcpProjectID = projectID

	lvl := parseLevel(level)

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Rename "msg" → "message" for GCP
			if a.Key == slog.MessageKey {
				a.Key = "message"
			}
			// Rename "level" → "severity" and uppercase the value for GCP
			if a.Key == slog.LevelKey {
				a.Key = "severity"
				if lvlVal, ok := a.Value.Any().(slog.Level); ok {
					a.Value = slog.StringValue(gcpSeverity(lvlVal))
				}
			}
			return a
		},
	})

	slog.SetDefault(slog.New(handler))
}

// NewLogger returns a logger with the given component name pre-attached.
// Use this for startup or component-level logging outside of HTTP request context.
func NewLogger(component string) *slog.Logger {
	return slog.Default().With(FieldComponent, component)
}

// GetProjectID returns the stored GCP project ID.
func GetProjectID() string {
	return gcpProjectID
}

// parseLevel converts a level string to slog.Level.
func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// gcpSeverity maps slog.Level to GCP-standard uppercase severity strings.
func gcpSeverity(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "ERROR"
	case l >= slog.LevelWarn:
		return "WARNING"
	case l >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}
