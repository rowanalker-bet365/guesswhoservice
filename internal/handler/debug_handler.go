package handler

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"

	"github.com/guesswho/internal/db"
)

// DebugHandler handles debug endpoints for development-time Redis inspection.
type DebugHandler struct {
	store  *db.Store
	apiKey string
}

// NewDebugHandler creates a new DebugHandler.
func NewDebugHandler(store *db.Store, apiKey string) *DebugHandler {
	return &DebugHandler{store: store, apiKey: apiKey}
}

// authorize checks the X-Debug-Key header against the configured API key.
// Returns false and writes the appropriate error response if the check fails.
func (h *DebugHandler) authorize(w http.ResponseWriter, r *http.Request) bool {
	if h.apiKey == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "debug endpoints are not configured"})
		return false
	}
	provided := r.Header.Get("X-Debug-Key")
	if subtle.ConstantTimeCompare([]byte(provided), []byte(h.apiKey)) != 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return false
	}
	return true
}

// GetTeamDebug handles GET /debug/team/{teamId}.
// It returns the raw team data stored in Redis for the given team ID,
// and also fetches the active session if one is set.
// This endpoint is only registered when APP_ENV != "production".
func (h *DebugHandler) GetTeamDebug(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	teamID := r.PathValue("teamId")
	if teamID == "" {
		http.Error(w, "teamId path parameter required", http.StatusBadRequest)
		return
	}

	team, err := h.store.ReadTeamData(r.Context(), teamID)
	if err != nil {
		http.Error(w, "Team not found: "+err.Error(), http.StatusNotFound)
		return
	}

	result := map[string]interface{}{
		"teamId": teamID,
		"team": map[string]interface{}{
			"name":             team.Name,
			"color":            team.Color,
			"registeredAt":     team.RegisteredAt,
			"score":            team.Score,
			"solves":           team.Solves,
			"fastestSolveMs":   team.FastestSolve.Milliseconds(),
			"solvedCharacters": team.SolvedCharacters,
			"milestones":       team.Milestones,
			"activeSessionId":  team.ActiveSessionID,
		},
	}

	if team.ActiveSessionID != "" {
		session, err := h.store.ReadSession(r.Context(), team.ActiveSessionID)
		if err == nil {
			result["session"] = session
		} else {
			result["sessionError"] = err.Error()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// FlushAll handles POST /debug/flush
// Wipes every key in Redis — use before a fresh hackathon run.
func (h *DebugHandler) FlushAll(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := h.store.FlushAll(r.Context()); err != nil {
		http.Error(w, "failed to flush redis: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "All Redis data has been flushed. Ready for a fresh run.",
	})
}