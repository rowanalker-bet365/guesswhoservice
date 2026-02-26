package logging

// Standardized field keys used across all log entries
const (
	FieldRequestID  = "requestId"
	FieldTraceID    = "logging.googleapis.com/trace"
	FieldSpanID     = "logging.googleapis.com/spanId"
	FieldMethod     = "method"
	FieldPath       = "path"
	FieldStatusCode = "statusCode"
	FieldDuration   = "durationMs"
	FieldTeamID     = "teamId"
	FieldSessionID  = "sessionId"
	FieldRemoteIP   = "remoteIp"
	FieldError      = "error"
	FieldComponent  = "component"
)
