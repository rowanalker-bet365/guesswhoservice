package handler

import (
	"encoding/json"
	"net/http"

	"github.com/guesswho/internal/repository"
)

// LeaderboardHandler handles leaderboard HTTP requests
type LeaderboardHandler struct {
	leaderboardRepo repository.LeaderboardRepository
}

// NewLeaderboardHandler creates a new leaderboard handler
func NewLeaderboardHandler(leaderboardRepo repository.LeaderboardRepository) *LeaderboardHandler {
	return &LeaderboardHandler{
		leaderboardRepo: leaderboardRepo,
	}
}

// GetLeaderboard handles GET /v1/leaderboard
func (h *LeaderboardHandler) GetLeaderboard(w http.ResponseWriter, r *http.Request) {
	entries := h.leaderboardRepo.GetLeaderboard()

	response := map[string]interface{}{
		"entries": entries,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
