package handler

import (
	"encoding/json"
	"net/http"
	"strings"
)

// APIError represents a structured error response for API consumers.
type APIError struct {
	Error     string `json:"error"`
	Message   string `json:"message"`
	SessionID string `json:"sessionId,omitempty"`
	Field     string `json:"field,omitempty"`
}

// writeErrorJSON writes a JSON error response with the given HTTP status code.
func writeErrorJSON(w http.ResponseWriter, statusCode int, apiErr APIError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(apiErr)
}

// classifyServiceError maps known service-layer error strings to HTTP status codes
// and structured error messages.
func classifyServiceError(err error, sessionID string, hints ...string) (int, APIError) {
	msg := err.Error()

	switch {
	case strings.Contains(msg, "session already completed"):
		return http.StatusConflict, APIError{
			Error:     "session_completed",
			Message:   "Session '" + sessionID + "' has already ended. No further actions can be performed. Start a new session with POST /sessions/start.",
			SessionID: sessionID,
		}
	case strings.Contains(msg, "invalid question ID"):
		questionID := ""
		if len(hints) > 0 {
			questionID = hints[0]
		}
		return http.StatusBadRequest, APIError{
			Error:   "invalid_question_id",
			Message: "Question ID '" + questionID + "' does not exist. Use GET /sessions/" + sessionID + "/questions to see available question IDs (T01-T64).",
		}
	case strings.Contains(msg, "no guesses remaining"):
		return http.StatusConflict, APIError{
			Error:     "no_guesses_remaining",
			Message:   "You have exhausted all guesses for session '" + sessionID + "'. The session is now over. Use POST /sessions/" + sessionID + "/reveal to see the target, then start a new session.",
			SessionID: sessionID,
		}
	case strings.Contains(msg, "invalid candidate ID"):
		candidateID := ""
		if len(hints) > 0 {
			candidateID = hints[0]
		}
		return http.StatusBadRequest, APIError{
			Error:   "invalid_candidate_id",
			Message: "Candidate ID '" + candidateID + "' is not valid. Use GET /sessions/" + sessionID + "/board to see valid candidate IDs.",
		}
	case strings.Contains(msg, "trait not found on target"):
		return http.StatusInternalServerError, APIError{
			Error:     "internal_error",
			Message:   "An internal error occurred while processing your question. Please retry or contact the event organiser.",
			SessionID: sessionID,
		}
	default:
		// Likely a redis.Nil / session-not-found error
		return http.StatusNotFound, APIError{
			Error:     "session_not_found",
			Message:   "No session exists with ID '" + sessionID + "'. The session may have expired or the ID is incorrect. Start a new session with POST /sessions/start.",
			SessionID: sessionID,
		}
	}
}
