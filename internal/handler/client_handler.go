package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	"sort"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/guesswho/internal/db"
	"github.com/guesswho/internal/middleware"
	"github.com/guesswho/internal/service"
)


// SignupRequest is the request body for the signup endpoint.
type SignupRequest struct {
	TeamName string `json:"teamName"`
	Password string `json:"password"`
}

// SignupResponse is the response body for the signup endpoint.
type SignupResponse struct {
	Message string `json:"message"`
}

// LoginRequest is the request body for the login endpoint.
type LoginRequest struct {
	TeamName string `json:"teamName"`
	Password string `json:"password"`
}

// LoginResponse is the response body for the login endpoint.
type LoginResponse struct {
	Token string `json:"token"`
	Team  struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
	} `json:"team"`
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

// ClientHandler holds dependencies for the client-facing API handlers.
type ClientHandler struct {
	store             *db.Store
	encryptionService service.EncryptionService
	jwtSecret         string
}

// NewClientHandler creates a new ClientHandler.
func NewClientHandler(store *db.Store, encryptionService service.EncryptionService, jwtSecret string) *ClientHandler {
	return &ClientHandler{
		store:             store,
		encryptionService: encryptionService,
		jwtSecret:         jwtSecret,
	}
}

// SignupHandler handles team creation
func (h *ClientHandler) SignupHandler(w http.ResponseWriter, r *http.Request) {
	var req SignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Check if team name already exists
	_, err := h.store.GetTeamIDByName(r.Context(), req.TeamName)
	if err == nil {
		http.Error(w, "Team name already exists", http.StatusConflict)
		return
	}

	teamID := "team-" + uuid.New().String()
	hashedPassword, err := h.encryptionService.HashPassword(req.Password)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	newTeam := &db.TeamData{
		Name:         req.TeamName,
		PasswordHash: hashedPassword,
		RegisteredAt: time.Now(),
		Color:        fmt.Sprintf("#%06x", uuid.New().ID()%0xFFFFFF),
	}

	if err := h.store.WriteTeamData(r.Context(), teamID, newTeam); err != nil {
		http.Error(w, "Failed to save team", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(SignupResponse{Message: "Team created successfully"})
}

// LoginHandler handles team authentication
func (h *ClientHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	teamID, err := h.store.GetTeamIDByName(r.Context(), req.TeamName)
	if err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	team, err := h.store.ReadTeamData(r.Context(), teamID)
	if err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	if !h.encryptionService.CheckPasswordHash(req.Password, team.PasswordHash) {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Create the JWT claims, which includes the team ID and expiry time
	claims := jwt.MapClaims{
		"teamId": teamID,
		"exp":    time.Now().Add(time.Hour * 72).Unix(),
	}

	// Create token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Generate encoded token and send it as response.
	signedToken, err := token.SignedString([]byte(h.jwtSecret))
	if err != nil {
		http.Error(w, "Failed to sign token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(LoginResponse{
		Token: signedToken,
		Team: struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Color string `json:"color"`
		}{
			ID:    teamID,
			Name:  team.Name,
			Color: team.Color,
		},
	})
}

// GetTeamProgressHandler returns the private progress for the authenticated team
func (h *ClientHandler) GetTeamProgressHandler(w http.ResponseWriter, r *http.Request) {
	teamID, ok := r.Context().Value(middleware.TeamIDKey).(string)
	if !ok {
		http.Error(w, "Team ID not found in token", http.StatusUnauthorized)
		return
	}

	team, err := h.store.ReadTeamData(r.Context(), teamID)
	if err != nil {
		http.Error(w, "Team not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(team)
}
// ResetTeamHandler resets the team's progress (solved characters and active session)
func (h *ClientHandler) ResetTeamHandler(w http.ResponseWriter, r *http.Request) {
	teamID, ok := r.Context().Value(middleware.TeamIDKey).(string)
	if !ok {
		http.Error(w, "Team ID not found in token", http.StatusUnauthorized)
		return
	}

	team, err := h.store.ReadTeamData(r.Context(), teamID)
	if err != nil {
		http.Error(w, "Team not found", http.StatusNotFound)
		return
	}

	// Reset progress
	team.SolvedCharacters = []string{}
	team.ActiveSessionID = ""

	if err := h.store.WriteTeamData(r.Context(), teamID, team); err != nil {
		http.Error(w, "Failed to reset team progress", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Team progress reset successfully"})
}

// GetLeaderboard handles GET /leaderboard
func (h *ClientHandler) GetLeaderboard(w http.ResponseWriter, r *http.Request) {
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

// GetMasterBoardHandler handles GET /game/master-board
func (h *ClientHandler) GetMasterBoardHandler(w http.ResponseWriter, r *http.Request) {
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