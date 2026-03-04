package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/guesswho/internal/db"
	"github.com/guesswho/internal/domain"
	"github.com/guesswho/internal/logging"
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
	ActiveSessionID     string               `json:"activeSessionId"`
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
		logging.Warn(r.Context(), "invalid signup request body", "error", err)
		writeErrorJSON(w, http.StatusBadRequest, APIError{
			Error:   "invalid_request_body",
			Message: "Could not parse request. Ensure you are sending valid JSON with Content-Type: application/json.",
		})
		return
	}

	if req.TeamName == "" {
		writeErrorJSON(w, http.StatusBadRequest, APIError{
			Error:   "missing_field",
			Message: "\"teamName\" is required.",
			Field:   "teamName",
		})
		return
	}
	if req.Password == "" {
		writeErrorJSON(w, http.StatusBadRequest, APIError{
			Error:   "missing_field",
			Message: "\"password\" is required.",
			Field:   "password",
		})
		return
	}

	teamID := "team-" + uuid.New().String()
	hashedPassword, err := h.encryptionService.HashPassword(req.Password)
	if err != nil {
		logging.Error(r.Context(), "failed to hash password", "error", err)
		writeErrorJSON(w, http.StatusInternalServerError, APIError{
			Error:   "internal_error",
			Message: "Failed to process signup. Please retry.",
		})
		return
	}

	// Atomically register team name — prevents TOCTOU race on duplicate names
	registered, err := h.store.RegisterTeamName(r.Context(), req.TeamName, teamID)
	if err != nil {
		logging.Error(r.Context(), "failed to register team name", "error", err)
		writeErrorJSON(w, http.StatusInternalServerError, APIError{
			Error:   "internal_error",
			Message: "Failed to register team. Please retry.",
		})
		return
	}
	if !registered {
		logging.Warn(r.Context(), "signup attempt with existing team name", "teamName", req.TeamName)
		writeErrorJSON(w, http.StatusConflict, APIError{
			Error:   "team_name_taken",
			Message: "The team name \"" + req.TeamName + "\" is already taken. Choose a different name.",
			Field:   "teamName",
		})
		return
	}

	newTeam := &db.TeamData{
		Name:         req.TeamName,
		PasswordHash: hashedPassword,
		RegisteredAt: time.Now(),
		Color:        fmt.Sprintf("#%06x", uuid.New().ID()%0xFFFFFF),
	}

	if err := h.store.WriteTeamData(r.Context(), teamID, newTeam); err != nil {
		logging.Error(r.Context(), "failed to save team", "teamId", teamID, "error", err)
		writeErrorJSON(w, http.StatusInternalServerError, APIError{
			Error:   "internal_error",
			Message: "Failed to save team. Please retry.",
		})
		return
	}

	logging.Info(r.Context(), "team created", "teamId", teamID, "teamName", req.TeamName)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(SignupResponse{Message: "Team created successfully"})
}

// LoginHandler handles team authentication
func (h *ClientHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logging.Warn(r.Context(), "invalid login request body", "error", err)
		writeErrorJSON(w, http.StatusBadRequest, APIError{
			Error:   "invalid_request_body",
			Message: "Could not parse request. Ensure you are sending valid JSON with Content-Type: application/json.",
		})
		return
	}

	if req.TeamName == "" || req.Password == "" {
		writeErrorJSON(w, http.StatusBadRequest, APIError{
			Error:   "missing_field",
			Message: "Both \"teamName\" and \"password\" are required.",
		})
		return
	}

	teamID, err := h.store.GetTeamIDByName(r.Context(), req.TeamName)
	if err != nil {
		logging.Warn(r.Context(), "login attempt for unknown team", "teamName", req.TeamName)
		writeErrorJSON(w, http.StatusUnauthorized, APIError{
			Error:   "invalid_credentials",
			Message: "Invalid team name or password.",
		})
		return
	}

	team, err := h.store.ReadTeamData(r.Context(), teamID)
	if err != nil {
		logging.Warn(r.Context(), "login attempt for unknown team", "teamName", req.TeamName)
		writeErrorJSON(w, http.StatusUnauthorized, APIError{
			Error:   "invalid_credentials",
			Message: "Invalid team name or password.",
		})
		return
	}

	if !h.encryptionService.CheckPasswordHash(req.Password, team.PasswordHash) {
		logging.Warn(r.Context(), "login failed - invalid password", "teamName", req.TeamName)
		writeErrorJSON(w, http.StatusUnauthorized, APIError{
			Error:   "invalid_credentials",
			Message: "Invalid team name or password.",
		})
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
		logging.Error(r.Context(), "failed to sign JWT", "teamId", teamID, "error", err)
		writeErrorJSON(w, http.StatusInternalServerError, APIError{
			Error:   "internal_error",
			Message: "Failed to process login. Please retry.",
		})
		return
	}

	logging.Info(r.Context(), "team logged in", "teamId", teamID, "teamName", req.TeamName)

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
		logging.Warn(r.Context(), "team ID not found in token")
		writeErrorJSON(w, http.StatusUnauthorized, APIError{
			Error:   "invalid_token",
			Message: "Your JWT token is missing or does not contain a valid team ID. Log in again via POST /client/auth/login.",
		})
		return
	}

	team, err := h.store.ReadTeamData(r.Context(), teamID)
	if err != nil {
		logging.Warn(r.Context(), "team not found for progress", "teamId", teamID, "error", err)
		writeErrorJSON(w, http.StatusNotFound, APIError{
			Error:   "team_not_found",
			Message: "No team found for the ID in your token. The team may have been deleted.",
		})
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
		ActiveSessionID:     team.ActiveSessionID,
	}
	if resp.SolvedCharacters == nil {
		resp.SolvedCharacters = []string{}
	}

	logging.Debug(r.Context(), "team progress retrieved", "teamId", teamID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// ResetTeamHandler resets the team's progress (solved characters and active session)
// using targeted Redis operations that do not overwrite other team fields.
func (h *ClientHandler) ResetTeamHandler(w http.ResponseWriter, r *http.Request) {
	teamID, ok := r.Context().Value(middleware.TeamIDKey).(string)
	if !ok {
		logging.Warn(r.Context(), "team ID not found in token for reset")
		writeErrorJSON(w, http.StatusUnauthorized, APIError{
			Error:   "invalid_token",
			Message: "Your JWT token is missing or does not contain a valid team ID. Log in again via POST /client/auth/login.",
		})
		return
	}

	if err := h.store.ResetTeamProgress(r.Context(), teamID); err != nil {
		logging.Error(r.Context(), "failed to reset team progress", "teamId", teamID, "error", err)
		writeErrorJSON(w, http.StatusInternalServerError, APIError{
			Error:   "internal_error",
			Message: "Failed to reset team progress. Please retry.",
		})
		return
	}

	logging.Info(r.Context(), "team progress reset", "teamId", teamID)

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
		logging.Error(r.Context(), "failed to get leaderboard", "error", err)
		writeErrorJSON(w, http.StatusInternalServerError, APIError{
			Error:   "internal_error",
			Message: "Failed to retrieve leaderboard. Please retry.",
		})
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
		logging.Error(r.Context(), "failed to get masterboard data", "error", err)
		writeErrorJSON(w, http.StatusInternalServerError, APIError{
			Error:   "internal_error",
			Message: "Failed to retrieve masterboard data. Please retry.",
		})
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
		logging.Warn(r.Context(), "failed to get team colors, using defaults", "error", err)
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
		imagePath := fmt.Sprintf("/images/%s.png", charID)
		if char, ok := h.characterCatalog.GetCharacterByID(charID); ok {
			imagePath = char.ImagePath
		}

		finalCharacters = append(finalCharacters, MasterBoardCharacterStatus{
			ID:            charID,
			ImagePath:     imagePath,
			SolvedByTeams: solvedByTeams,
		})
	}

	// Sort by character ID to guarantee a stable order across polls.
	// Go map iteration is non-deterministic; without sorting, the characters
	// would appear at different grid positions on every refresh.
	sort.Slice(finalCharacters, func(i, j int) bool {
		return finalCharacters[i].ID < finalCharacters[j].ID
	})

	response := MasterBoardResponse{
		Characters: finalCharacters,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
