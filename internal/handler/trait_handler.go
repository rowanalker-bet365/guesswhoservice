package handler

import (
	"encoding/json"
	"net/http"

	"github.com/guesswho/internal/domain"
	"github.com/guesswho/internal/middleware"
	"github.com/guesswho/internal/service"
)

// TraitHandler handles trait-related HTTP requests
type TraitHandler struct {
	traitCatalog      service.TraitCatalogService
	encryptionService service.EncryptionService
	milestoneService  service.MilestoneService
}

// NewTraitHandler creates a new trait handler
func NewTraitHandler(traitCatalog service.TraitCatalogService, encryptionService service.EncryptionService, milestoneService service.MilestoneService) *TraitHandler {
	return &TraitHandler{
		traitCatalog:      traitCatalog,
		encryptionService: encryptionService,
		milestoneService:  milestoneService,
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
			"cost":       trait.Cost,
			"tier":       trait.Tier,
		}
		questions = append(questions, question)
	}

	response := map[string]interface{}{
		"questions": questions,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// DecodeRequest represents a request to decode an encrypted answer
type DecodeRequest struct {
	Encrypted string `json:"encrypted"`
}

// DecodeResponse represents the response from decoding
type DecodeResponse struct {
	Decrypted string `json:"decrypted"`
}

// Decode handles POST /sessions/{sessionId}/decode
func (h *TraitHandler) Decode(w http.ResponseWriter, r *http.Request) {
	var req DecodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	plaintext, err := h.encryptionService.Decrypt(req.Encrypted)
	if err != nil {
		http.Error(w, "Failed to decode: "+err.Error(), http.StatusBadRequest)
		return
	}

	// M5: Encrypted Answer Handled — awarded once when a team successfully decodes
	// an encrypted trait answer. The teamID is extracted from the JWT context.
	if teamID, ok := r.Context().Value(middleware.TeamIDKey).(string); ok && teamID != "" {
		h.milestoneService.AwardIfAbsent(r.Context(), teamID, domain.MilestoneM5)
	}

	response := DecodeResponse{
		Decrypted: plaintext,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
