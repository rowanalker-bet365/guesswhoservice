package handler

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/guesswho/internal/db"
)

// LeaderboardHandler handles leaderboard HTTP requests
type LeaderboardHandler struct {
	store *db.Store
}

// NewLeaderboardHandler creates a new leaderboard handler
func NewLeaderboardHandler(store *db.Store) *LeaderboardHandler {
	return &LeaderboardHandler{
		store: store,
	}
}

// GetLeaderboard handles GET /v1/leaderboard
func (h *LeaderboardHandler) GetLeaderboard(w http.ResponseWriter, r *http.Request) {
	teamIDs, err := h.store.GetAllTeamIDs(r.Context())
	if err != nil {
		http.Error(w, "Failed to get team IDs for leaderboard", http.StatusInternalServerError)
		return
	}

	var teams []*db.TeamData
	for _, teamID := range teamIDs {
		team, err := h.store.ReadTeamData(r.Context(), teamID)
		if err != nil {
			// Log the error but continue; one failing team shouldn't kill the whole leaderboard
			continue
		}
		teams = append(teams, team)
	}

	sort.Slice(teams, func(i, j int) bool {
		return teams[i].Score > teams[j].Score
	})

	response := map[string]interface{}{
		"entries": teams,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
