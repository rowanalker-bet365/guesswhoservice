package handler

import (
	"encoding/json"
	"net/http"

	"github.com/guesswho/internal/domain"
	"github.com/guesswho/internal/logging"
	"github.com/guesswho/internal/service"
)

// TraitHandler handles trait-related HTTP requests
type TraitHandler struct {
	traitCatalog      service.TraitCatalogService
	milestoneService  service.MilestoneService
	sessionService    service.SessionService
	encryptionService service.EncryptionService
}

// NewTraitHandler creates a new trait handler
func NewTraitHandler(
	traitCatalog service.TraitCatalogService,
	milestoneService service.MilestoneService,
	sessionService service.SessionService,
	encryptionService service.EncryptionService,
) *TraitHandler {
	return &TraitHandler{
		traitCatalog:      traitCatalog,
		milestoneService:  milestoneService,
		sessionService:    sessionService,
		encryptionService: encryptionService,
	}
}

// GetQuestions handles GET /sessions/{sessionId}/questions
func (h *TraitHandler) GetQuestions(w http.ResponseWriter, r *http.Request) {
	traits := h.traitCatalog.GetAllTraits()

	questions := make([]map[string]interface{}, 0, len(traits))
	for _, trait := range traits {
		question := map[string]interface{}{
			"questionId": trait.QuestionID,
			"traitKey":   trait.TraitKey,
			"type":       trait.Type,
		}
		questions = append(questions, question)
	}

	response := map[string]interface{}{
		"questions": questions,
	}

	logging.Debug(r.Context(), "questions retrieved", "count", len(traits))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// DecodeRequest represents a request to get decryption info for an encrypted answer
type DecodeRequest struct {
	QuestionID string `json:"questionId"`
	Encrypted  string `json:"encrypted"`
}

// DecodeResponse represents the cipher hint response for client-side decryption
type DecodeResponse struct {
	Cipher   string `json:"cipher"`
	Key      string `json:"key,omitempty"`
	Encoding string `json:"encoding"`
	Hint     string `json:"hint"`
}

// Decode handles POST /sessions/{sessionId}/decode
// Accepts the questionId and encrypted value, returns cipher info so the client can decrypt.
func (h *TraitHandler) Decode(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionId")
	if sessionID == "" {
		writeErrorJSON(w, http.StatusBadRequest, APIError{
			Error:   "missing_session_id",
			Message: "The sessionId path parameter is required.",
		})
		return
	}

	var req DecodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, APIError{
			Error:   "invalid_request_body",
			Message: "The request body could not be parsed as JSON. Expected format: {\"questionId\": \"T01\", \"encrypted\": \"<encrypted_value>\"}.",
		})
		return
	}

	if req.QuestionID == "" {
		writeErrorJSON(w, http.StatusBadRequest, APIError{
			Error:   "missing_question_id",
			Message: "The questionId field is required. Provide the question ID of the encrypted answer you want to decode.",
		})
		return
	}

	if req.Encrypted == "" {
		writeErrorJSON(w, http.StatusBadRequest, APIError{
			Error:   "missing_encrypted_value",
			Message: "The encrypted field is required. Provide the encrypted answer value you received from the ask endpoint.",
		})
		return
	}

	// L4 (known gap): the encrypted field is validated for presence but its value is
	// not verified against the stored ciphertext because the service does not persist
	// per-question ciphertexts. A player could supply any non-empty string and still
	// receive the session key. Fixing this fully would require storing the ciphertext
	// at question-answer time — a larger structural change deferred to a future iteration.
	// At minimum, log a warning so operators can observe suspicious decode calls.
	logging.Warn(r.Context(), "decode: encrypted field value not verified against stored ciphertext (known gap L4)",
		"sessionId", sessionID, "questionId", req.QuestionID)

	session, err := h.sessionService.GetSession(r.Context(), sessionID)
	if err != nil {
		logging.Warn(r.Context(), "decode: session not found", "sessionId", sessionID, "error", err)
		writeErrorJSON(w, http.StatusNotFound, APIError{
			Error:     "session_not_found",
			Message:   "No session exists with ID '" + sessionID + "'. The session may have expired or the ID is incorrect.",
			SessionID: sessionID,
		})
		return
	}

	// Verify the question was actually encrypted in this session
	if decided, exists := session.EncryptedQuestions[req.QuestionID]; !exists || !decided {
		writeErrorJSON(w, http.StatusBadRequest, APIError{
			Error:   "not_encrypted",
			Message: "Question '" + req.QuestionID + "' was not encrypted in this session. Only questions with status 'encrypted' need decoding.",
		})
		return
	}

	hint := h.encryptionService.GetCipherInfo(session.EncryptCipher, session.EncryptKey)

	// M5: Encrypted Answer Handled — awarded once when a team successfully calls decode.
	h.milestoneService.AwardIfAbsent(r.Context(), session.TeamID, domain.MilestoneM5)

	logging.Debug(r.Context(), "decode info returned", "sessionId", sessionID, "questionId", req.QuestionID, "cipher", session.EncryptCipher)

	response := DecodeResponse{
		Cipher:   hint.Cipher,
		Key:      hint.Key,
		Encoding: hint.Encoding,
		Hint:     hint.Hint,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
