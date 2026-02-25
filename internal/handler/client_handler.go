package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/guesswho/internal/db"
	"github.com/guesswho/internal/domain"
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

// TeamProgressResponse is the JSON shape returned by GetTeamProgressHandler.
// Field names and types match the UI's TeamData interface exactly.
type TeamProgressResponse struct {
	ID                  string               `json:"id"`
	TeamName            string               `json:"teamName"`
	TeamColor           string               `json:"teamColor"`
	ChallengeStartTime  string               `json:"challengeStartTime"`
	TotalSolves         int                  `json:"totalSolves"`
	SolvedCharacters    []string             `json:"solvedCharacters"`
	FastestSolve        int64                `json:"fastestSolve"` // milliseconds
	TotalScore          int                  `json:"totalScore"`
	CompletedMilestones []CompletedMilestone `json:"completedMilestones"`
}

// CompletedMilestone is a single milestone entry in TeamProgressResponse.
type CompletedMilestone struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Score     int    `json:"score"`
	TimeTaken string `json:"timeTaken"`
}

// MasterBoardCharacterStatus represents the status of a character on the master board
type MasterBoardCharacterStatus struct {
	ID            string `json:"id"`
	ImagePath     string `json:"imagePath"`
	SolvedByTeams []struct {
		TeamID string `json:"teamId"`
		Color  string `json:"color"`
	} `json:"solvedByTeams"`
}

// MasterBoardResponse represents the response for the master board endpoint
type MasterBoardResponse struct {
	Characters []MasterBoardCharacterStatus `json:"characters"`
}

// ClientHandler holds dependencies for the client-facing API handlers.
type ClientHandler struct {
	store             *db.Store
	encryptionService service.EncryptionService
	characterCatalog  service.CharacterCatalogService
	jwtSecret         string
}

// NewClientHandler creates a new ClientHandler.
func NewClientHandler(store *db.Store, encryptionService service.EncryptionService, characterCatalog service.CharacterCatalogService, jwtSecret string) *ClientHandler {
	return &ClientHandler{
		store:             store,
		encryptionService: encryptionService,
		characterCatalog:  characterCatalog,
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

// GetTeamProgressHandler returns the private progress for the authenticated team.
// The response is shaped to match the UI's TeamData interface.
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

	// Build a lookup map from milestone ID → name + score using the domain metadata.
	type milestoneMeta struct {
		Name  string
		Score int
	}
	metaByID := make(map[string]milestoneMeta, len(domain.AllMilestones))
	for _, mi := range domain.AllMilestones {
		metaByID[string(mi.ID)] = milestoneMeta{Name: mi.Name, Score: mi.Score}
	}

	// Convert milestones []string → []CompletedMilestone with name + score populated.
	milestones := make([]CompletedMilestone, 0, len(team.Milestones))
	for _, m := range team.Milestones {
		if m != "" {
			meta := metaByID[m]
			milestones = append(milestones, CompletedMilestone{
				ID:        m,
				Name:      meta.Name,
				Score:     meta.Score,
				TimeTaken: "",
			})
		}
	}

	resp := TeamProgressResponse{
		ID:                  teamID,
		TeamName:            team.Name,
		TeamColor:           team.Color,
		ChallengeStartTime:  team.RegisteredAt.Format(time.RFC3339),
		TotalSolves:         team.Solves,
		SolvedCharacters:    team.SolvedCharacters,
		FastestSolve:        team.FastestSolve.Milliseconds(),
		TotalScore:          team.Score,
		CompletedMilestones: milestones,
	}
	if resp.SolvedCharacters == nil {
		resp.SolvedCharacters = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
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

// GetLeaderboard handles GET /leaderboard.
// It reads rankings directly from the "leaderboard" sorted set (ZREVRANGE … WITHSCORES)
// and pipelines team detail lookups to avoid N+1 Redis round-trips.
func (h *ClientHandler) GetLeaderboard(w http.ResponseWriter, r *http.Request) {
	entries, err := h.store.GetLeaderboardEntries(r.Context())
	if err != nil {
		http.Error(w, "Failed to get leaderboard", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"entries": entries,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetMasterBoardHandler handles GET /game/master-board.
// It reads from the "masterboard:<character_id>" sets that are populated atomically
// by the Lua script on each correct solve, rather than iterating over all teams.
func (h *ClientHandler) GetMasterBoardHandler(w http.ResponseWriter, r *http.Request) {
	// GetMasterboardFromSets uses HKEYS "masterboard" for the full character list,
	// then pipelines SMEMBERS "masterboard:<id>" for each character.
	masterboardData, err := h.store.GetMasterboardFromSets(r.Context())
	if err != nil {
		http.Error(w, "Failed to get masterboard data", http.StatusInternalServerError)
		return
	}

	// Collect the unique set of team IDs that appear anywhere on the masterboard
	// so we can batch the color lookups in a single pipeline.
	teamIDSet := make(map[string]struct{})
	for _, solvedBy := range masterboardData {
		for _, teamID := range solvedBy {
			teamIDSet[teamID] = struct{}{}
		}
	}
	uniqueTeamIDs := make([]string, 0, len(teamIDSet))
	for teamID := range teamIDSet {
		uniqueTeamIDs = append(uniqueTeamIDs, teamID)
	}

	// Fetch all team colors in one pipelined batch via the store helper.
	teamColors, err := h.store.GetTeamColors(r.Context(), uniqueTeamIDs)
	if err != nil {
		// Non-fatal: fall back to default colors rather than failing the whole request.
		teamColors = map[string]string{}
	}

	var finalCharacters []MasterBoardCharacterStatus
	for charID, solvedBy := range masterboardData {
		solvedByTeams := make([]struct {
			TeamID string `json:"teamId"`
			Color  string `json:"color"`
		}, 0, len(solvedBy))

		for _, teamID := range solvedBy {
			color := teamColors[teamID]
			if color == "" {
				color = "#000000"
			}
			solvedByTeams = append(solvedByTeams, struct {
				TeamID string `json:"teamId"`
				Color  string `json:"color"`
			}{
				TeamID: teamID,
				Color:  color,
			})
		}

		// Look up the canonical image path from the character catalog.
		// Fall back to a predictable default if the character is not found.
		imagePath := fmt.Sprintf("/public/images/%s.png", charID)
		if char, ok := h.characterCatalog.GetCharacterByID(charID); ok {
			imagePath = char.ImagePath
		}

		finalCharacters = append(finalCharacters, MasterBoardCharacterStatus{
			ID:            charID,
			ImagePath:     imagePath,
			SolvedByTeams: solvedByTeams,
		})
	}

	response := MasterBoardResponse{
		Characters: finalCharacters,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
