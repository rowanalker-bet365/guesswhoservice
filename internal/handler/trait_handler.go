package handler

import (
	"encoding/json"
	"net/http"

	"github.com/guesswho/internal/logging"
	"github.com/guesswho/internal/service"
)

// TraitHandler handles trait-related HTTP requests
type TraitHandler struct {
	traitCatalog     service.TraitCatalogService
	milestoneService service.MilestoneService
	sessionService   service.SessionService
}

// NewTraitHandler creates a new trait handler
func NewTraitHandler(traitCatalog service.TraitCatalogService, milestoneService service.MilestoneService, sessionService service.SessionService) *TraitHandler {
	return &TraitHandler{
		traitCatalog:     traitCatalog,
		milestoneService: milestoneService,
		sessionService:   sessionService,
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

