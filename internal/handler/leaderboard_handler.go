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

// MasterBoardCharacterStatus represents the status of a character on the master board
type MasterBoardCharacterStatus struct {
	ID            string   `json:"id"`
	SolvedBy      []string `json:"solvedBy"`
	IsSolvedByAll bool     `json:"isSolvedByAll"`
}

// MasterBoardResponse represents the response for the master board endpoint
type MasterBoardResponse struct {
	TotalTeams int                                   `json:"totalTeams"`
	Characters map[string]MasterBoardCharacterStatus `json:"characters"`
}

// GetMasterBoardHandler handles GET /v1/game/master-board
func (h *LeaderboardHandler) GetMasterBoardHandler(w http.ResponseWriter, r *http.Request) {
	teamIDs, err := h.store.GetAllTeamIDs(r.Context())
	if err != nil {
		http.Error(w, "Failed to get team IDs for master board", http.StatusInternalServerError)
		return
	}

	totalTeams := len(teamIDs)
	characterMap := make(map[string]*MasterBoardCharacterStatus)

	for _, teamID := range teamIDs {
		team, err := h.store.ReadTeamData(r.Context(), teamID)
		if err != nil {
			// Log error but continue
			continue
		}

		for _, charID := range team.SolvedCharacters {
			if _, exists := characterMap[charID]; !exists {
				characterMap[charID] = &MasterBoardCharacterStatus{
					ID:       charID,
					SolvedBy: []string{},
				}
			}
			characterMap[charID].SolvedBy = append(characterMap[charID].SolvedBy, teamID)
		}
	}

	// Finalize the map values (calculate IsSolvedByAll)
	finalCharacters := make(map[string]MasterBoardCharacterStatus)
	for charID, status := range characterMap {
		status.IsSolvedByAll = len(status.SolvedBy) == totalTeams && totalTeams > 0
		finalCharacters[charID] = *status
	}

	response := MasterBoardResponse{
		TotalTeams: totalTeams,
		Characters: finalCharacters,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
