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
			Message:   "This session has concluded. Start a new session with POST /sessions/start.",
			SessionID: sessionID,
		}
	case strings.Contains(msg, "enum trait requires a value"):
		questionID := ""
		if len(hints) > 0 {
			questionID = hints[0]
		}
		return http.StatusBadRequest, APIError{
			Error:   "value_required",
			Message: "This is an enum trait with multiple possible values. You must include a \"value\" field — e.g. {\"questionId\": \"" + questionID + "\", \"value\": \"brown\"}. Boolean traits do not require a value — e.g. {\"questionId\": \"T04\"}. Consult GET /sessions/{sessionId}/questions for trait types.",
		}
	case strings.Contains(msg, "invalid question ID"):
		questionID := ""
		if len(hints) > 0 {
			questionID = hints[0]
		}
		return http.StatusBadRequest, APIError{
			Error:   "invalid_question_id",
			Message: "'" + questionID + "' is not a recognised question. Consult GET /sessions/" + sessionID + "/questions for the available set.",
		}
	case strings.Contains(msg, "no guesses remaining"):
		return http.StatusConflict, APIError{
			Error:     "no_guesses_remaining",
			Message:   "All guesses have been spent. The session is over. You may POST /sessions/" + sessionID + "/reveal to learn the target's identity, then start fresh.",
			SessionID: sessionID,
		}
	case strings.Contains(msg, "invalid candidate ID"):
		candidateID := ""
		if len(hints) > 0 {
			candidateID = hints[0]
		}
		return http.StatusBadRequest, APIError{
			Error:   "invalid_candidate_id",
			Message: "'" + candidateID + "' does not match any candidate on the board. Consult GET /sessions/" + sessionID + "/board for valid IDs.",
		}
	case strings.Contains(msg, "must ask at least 5 questions"):
		return http.StatusConflict, APIError{
			Error:     "insufficient_questions",
			Message:   "You must ask more questions before you can reveal the target.",
			SessionID: sessionID,
		}
	case strings.Contains(msg, "failed to encrypt answer"),
		strings.Contains(msg, "failed to persist session"):
		return http.StatusInternalServerError, APIError{
			Error:     "transient_error",
			Message:   "A transient server error occurred. Please retry your last request.",
			SessionID: sessionID,
		}
	case strings.Contains(msg, "trait not found on target"):
		return http.StatusInternalServerError, APIError{
			Error:     "internal_error",
			Message:   "An internal error occurred. Please retry or contact the event organiser.",
			SessionID: sessionID,
		}
	case strings.Contains(msg, "not_stage_2"):
		return http.StatusBadRequest, APIError{
			Error:     "not_stage_2",
			Message:   "The /decrypt endpoint is not available for this session. It unlocks at a later stage of the game.",
			SessionID: sessionID,
		}
	case strings.Contains(msg, "no_pending_decryption"):
		return http.StatusConflict, APIError{
			Error:     "no_pending_decryption",
			Message:   "There is no cipher to break. This operation is only available when an encrypted response is pending.",
			SessionID: sessionID,
		}
	case strings.Contains(msg, "wrong_decryption"):
		return http.StatusUnprocessableEntity, APIError{
			Error:     "wrong_decryption",
			Message:   "Decryption failed. Re-examine your key derivation and cipher implementation, then try again.",
			SessionID: sessionID,
		}
	case strings.Contains(msg, "session_locked"):
		return http.StatusServiceUnavailable, APIError{
			Error:     "session_locked",
			Message:   "This session is currently being modified by another request. Retry shortly.",
			SessionID: sessionID,
		}
	case strings.Contains(msg, "pending_decryption"):
		return http.StatusConflict, APIError{
			Error:     "pending_decryption",
			Message:   "An encrypted challenge is awaiting your response. Resolve it via POST /sessions/" + sessionID + "/decrypt before making another guess.",
			SessionID: sessionID,
		}
	case strings.Contains(msg, "team not found"):
		return http.StatusForbidden, APIError{
			Error:   "team_not_found",
			Message: "The provided team ID does not correspond to a registered team. Verify your X-Team-Id header.",
		}
	default:
		// Likely a redis.Nil / session-not-found error
		return http.StatusNotFound, APIError{
			Error:     "session_not_found",
			Message:   "No session exists with this ID. It may have expired or the ID is incorrect. Start a new session with POST /sessions/start.",
			SessionID: sessionID,
		}
	}
}
