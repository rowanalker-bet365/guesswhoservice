package handler

import (
	"encoding/json"
	"net/http"

	"github.com/guesswho/internal/service"
)

// TraitHandler handles trait-related HTTP requests
type TraitHandler struct {
	traitCatalog      service.TraitCatalogService
	encryptionService service.EncryptionService
}

// NewTraitHandler creates a new trait handler
func NewTraitHandler(traitCatalog service.TraitCatalogService, encryptionService service.EncryptionService) *TraitHandler {
	return &TraitHandler{
		traitCatalog:      traitCatalog,
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

	response := DecodeResponse{
		Decrypted: plaintext,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
