package handler

import (
	"encoding/json"
	"net/http"

	"github.com/guesswho/internal/domain"
	"github.com/guesswho/internal/logging"
	"github.com/guesswho/internal/middleware"
	"github.com/guesswho/internal/service"
)

// TraitHandler handles trait-related HTTP requests
type TraitHandler struct {
	traitCatalog      service.TraitCatalogService
	encryptionService service.EncryptionService
	milestoneService  service.MilestoneService
	sessionService    service.SessionService
}

// NewTraitHandler creates a new trait handler
func NewTraitHandler(traitCatalog service.TraitCatalogService, encryptionService service.EncryptionService, milestoneService service.MilestoneService, sessionService service.SessionService) *TraitHandler {
	return &TraitHandler{
		traitCatalog:      traitCatalog,
		encryptionService: encryptionService,
		milestoneService:  milestoneService,
		sessionService:    sessionService,
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

// DecodeRequest represents a request to get cipher information for a session
type DecodeRequest struct {
	Encrypted string `json:"encrypted"`
}

// DecodeResponse represents the cipher hint response for client-side decryption
type DecodeResponse struct {
	Cipher   string `json:"cipher"`
	Key      string `json:"key"`
	Encoding string `json:"encoding"`
	Hint     string `json:"hint"`
}

// Decode handles POST /sessions/{sessionId}/decode
// It returns the session's cipher and key so the client can decrypt answers locally.
func (h *TraitHandler) Decode(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionId")
	if sessionID == "" {
		http.Error(w, "Missing sessionId", http.StatusBadRequest)
		return
	}

	session, err := h.sessionService.GetSession(r.Context(), sessionID)
	if err != nil {
		logging.Warn(r.Context(), "decode: session not found", "sessionId", sessionID, "error", err)
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	hint := h.encryptionService.GetCipherInfo(session.EncryptCipher, session.EncryptKey)

	// M5: Encrypted Answer Handled — awarded once when a team successfully calls decode.
	if teamID, ok := r.Context().Value(middleware.TeamIDKey).(string); ok && teamID != "" {
		h.milestoneService.AwardIfAbsent(r.Context(), teamID, domain.MilestoneM5)
	}

	logging.Debug(r.Context(), "decode info returned", "sessionId", sessionID, "cipher", session.EncryptCipher)

	response := DecodeResponse{
		Cipher:   hint.Cipher,
		Key:      hint.Key,
		Encoding: hint.Encoding,
		Hint:     hint.Hint,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
