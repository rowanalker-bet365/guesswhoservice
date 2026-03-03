package handler

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/guesswho/internal/db"
	"github.com/guesswho/internal/logging"
	"github.com/guesswho/internal/service"
)

// DebugHandler handles debug endpoints for development-time Redis inspection.
type DebugHandler struct {
	store             *db.Store
	apiKey            string
	encryptionService service.EncryptionService
}

// NewDebugHandler creates a new DebugHandler.
func NewDebugHandler(store *db.Store, apiKey string, encryptionService service.EncryptionService) *DebugHandler {
	return &DebugHandler{store: store, apiKey: apiKey, encryptionService: encryptionService}
}

// authorize checks the X-Debug-Key header against the configured API key.
// Returns false and writes the appropriate error response if the check fails.
func (h *DebugHandler) authorize(w http.ResponseWriter, r *http.Request) bool {
	if h.apiKey == "" {
		logging.Warn(r.Context(), "debug endpoint accessed but not configured")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "debug endpoints are not configured"})
		return false
	}
	provided := r.Header.Get("X-Debug-Key")
	if subtle.ConstantTimeCompare([]byte(provided), []byte(h.apiKey)) != 1 {
		logging.Warn(r.Context(), "unauthorized debug access attempt")
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
		logging.Warn(r.Context(), "missing teamId for debug")
		http.Error(w, "teamId path parameter required", http.StatusBadRequest)
		return
	}

	team, err := h.store.ReadTeamData(r.Context(), teamID)
	if err != nil {
		logging.Warn(r.Context(), "team not found for debug", "teamId", teamID, "error", err)
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

	logging.Info(r.Context(), "debug data retrieved", "teamId", teamID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// FlushAll handles POST /debug/flush
// Wipes every key in Redis — use before a fresh hackathon run.
// After flushing, the masterboard HASH is immediately re-seeded so that
// GET /client/game/master-board never returns {"characters":null} after a flush.
func (h *DebugHandler) FlushAll(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := h.store.FlushAll(r.Context()); err != nil {
		logging.Error(r.Context(), "failed to flush Redis", "error", err)
		http.Error(w, "failed to flush redis: "+err.Error(), http.StatusInternalServerError)
		return
	}
	logging.Warn(r.Context(), "Redis flushed - all data deleted")

	// Re-seed the masterboard HASH immediately so the system is never left in a
	// broken state where HKeys("masterboard") returns zero results.
	if err := h.store.InitializeMasterboard(r.Context()); err != nil {
		logging.Error(r.Context(), "failed to re-initialize masterboard after flush", "error", err)
		// Non-fatal: the self-healing in GetMasterboardFromSets will recover on the
		// next request, but log the error so operators are aware.
	} else {
		logging.Info(r.Context(), "masterboard re-initialized after flush")
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "All Redis data has been flushed. Ready for a fresh run.",
	})
}

// DecryptHandler handles POST /debug/decrypt.
// It decrypts a ciphertext using the specified cipher type and key.
func (h *DebugHandler) DecryptHandler(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}

	var req struct {
		Ciphertext string `json:"ciphertext"`
		CipherType string `json:"cipherType"`
		KeyHex     string `json:"keyHex"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, APIError{
			Error:   "invalid_request",
			Message: "Invalid JSON body: " + err.Error(),
		})
		return
	}

	if req.Ciphertext == "" {
		writeErrorJSON(w, http.StatusBadRequest, APIError{
			Error:   "missing_field",
			Message: "Field 'ciphertext' is required.",
			Field:   "ciphertext",
		})
		return
	}
	if req.CipherType == "" {
		writeErrorJSON(w, http.StatusBadRequest, APIError{
			Error:   "missing_field",
			Message: "Field 'cipherType' is required.",
			Field:   "cipherType",
		})
		return
	}

	plaintext, err := h.encryptionService.Decrypt(req.Ciphertext, req.CipherType, req.KeyHex)
	if err != nil {
		writeErrorJSON(w, http.StatusBadRequest, APIError{
			Error:   "decrypt_failed",
			Message: err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"plaintext":  plaintext,
		"cipherType": req.CipherType,
	})
}

// ListTeamsDebug handles GET /debug/teams.
// Returns debug data for every registered team, keyed by team name for readability.
func (h *DebugHandler) ListTeamsDebug(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}

	ctx := r.Context()
	teamIDs, err := h.store.GetAllTeamIDs(ctx)
	if err != nil {
		logging.Error(ctx, "failed to list team IDs", "error", err)
		http.Error(w, "failed to list teams: "+err.Error(), http.StatusInternalServerError)
		return
	}

	teams := make(map[string]map[string]interface{}, len(teamIDs))
	for _, teamID := range teamIDs {
		team, err := h.store.ReadTeamData(ctx, teamID)
		if err != nil {
			continue
		}

		data := map[string]interface{}{
			"color":            team.Color,
			"registeredAt":     team.RegisteredAt,
			"score":            team.Score,
			"solves":           team.Solves,
			"solvedCharacters": team.SolvedCharacters,
			"milestones":       team.Milestones,
		}

		if team.ActiveSessionID != "" {
			session, err := h.store.ReadSession(ctx, team.ActiveSessionID)
			if err == nil {
				var targetFakeID string
				if session.TargetCandidate != nil && session.CandidateIDMapRev != nil {
					targetFakeID = session.CandidateIDMapRev[session.TargetCandidate.CandidateID]
				}
				data["activeSession"] = map[string]interface{}{
					"targetCandidate":    targetFakeID,
					"questionsAsked":     session.QuestionsAsked,
					"failedRequests":     session.FailedRequests,
					"timeouts":           session.Timeouts,
					"unhandled5xx":       session.Unhandled5xx,
					"guessesRemaining":   session.GuessesRemaining,
					"completed":          session.Completed,
					"correctGuess":       session.CorrectGuess,
					"guessedCandidates":  session.GuessedCandidates,
					"encryptKey":         session.EncryptKey,
					"encryptCipher":      session.EncryptCipher,
					"encryptedQuestions": session.EncryptedQuestions,
					"blockedQuestions":   session.BlockedQuestions,
				}
			}
		}

		teams[team.Name] = map[string]interface{}{
			"teamId": teamID,
			"data":   data,
		}
	}

	logging.Info(ctx, "debug teams listed", "count", len(teams))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count": len(teams),
		"teams": teams,
	})
}

// DeleteTeamDebug handles DELETE /debug/team/{teamId}.
// Deletes a team and all its associated Redis data.
func (h *DebugHandler) DeleteTeamDebug(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}

	teamID := r.PathValue("teamId")
	if teamID == "" {
		logging.Warn(r.Context(), "missing teamId for debug delete")
		http.Error(w, "teamId path parameter required", http.StatusBadRequest)
		return
	}

	if err := h.store.DeleteTeam(r.Context(), teamID); err != nil {
		if errors.Is(err, db.ErrTeamNotFound) {
			logging.Warn(r.Context(), "team not found for delete", "teamId", teamID)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "team not found: " + teamID})
			return
		}
		logging.Error(r.Context(), "failed to delete team", "teamId", teamID, "error", err)
		http.Error(w, "failed to delete team: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logging.Warn(r.Context(), "team deleted via debug endpoint", "teamId", teamID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Team " + teamID + " and all associated data deleted.",
	})
}
