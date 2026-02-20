package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/guesswho/internal/middleware"
	"github.com/guesswho/internal/repository"
	"github.com/guesswho/internal/service"
)

// In-memory store for teams, for now.
var (
	teams = make(map[string]*TeamData)
	mu    sync.RWMutex
)

// TeamData is the central model for a team's private state.
type TeamData struct {
	ID                 string    `json:"id"`
	TeamName           string    `json:"teamName"`
	TeamColor          string    `json:"teamColor"`
	ChallengeStartTime time.Time `json:"challengeStartTime"`
	TotalSolves        int       `json:"totalSolves"`
	SolvedCharacters   []string  `json:"solvedCharacters"`
	FastestSolve       int       `json:"fastestSolve"` // Duration in milliseconds
	TotalScore         int       `json:"totalScore"`
	Password           string    `json:"-"` // This will not be exposed in the API
}

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
	SessionID string `json:"sessionId"`
}

// ClientHandler holds dependencies for the client-facing API handlers.
type ClientHandler struct {
	sessionService   service.SessionService
	leaderboardRepo  repository.LeaderboardRepository
	traitCatalog      service.TraitCatalogService
	encryptionService service.EncryptionService
	jwtSecret         string
}

// NewClientHandler creates a new ClientHandler.
func NewClientHandler(sessionService service.SessionService, leaderboardRepo repository.LeaderboardRepository, traitCatalog service.TraitCatalogService, encryptionService service.EncryptionService, jwtSecret string) *ClientHandler {
	return &ClientHandler{
		sessionService:    sessionService,
		leaderboardRepo:   leaderboardRepo,
		traitCatalog:      traitCatalog,
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

	mu.RLock()
	for _, team := range teams {
		if team.TeamName == req.TeamName {
			mu.RUnlock()
			http.Error(w, "Team name already exists", http.StatusConflict)
			return
		}
	}
	mu.RUnlock()

	newTeam := &TeamData{
		ID:                 "team-" + uuid.New().String(),
		TeamName:           req.TeamName,
		Password:           req.Password, // In a real app, hash this!
		TeamColor:          fmt.Sprintf("#%06x", uuid.New().ID()%0xFFFFFF),
		ChallengeStartTime: time.Now(),
	}

	mu.Lock()
	teams[newTeam.ID] = newTeam
	mu.Unlock()

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

	var foundTeam *TeamData
	mu.RLock()
	for _, team := range teams {
		if team.TeamName == req.TeamName && team.Password == req.Password {
			foundTeam = team
			break
		}
	}
	mu.RUnlock()

	if foundTeam == nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Create the JWT claims, which includes the team ID and expiry time
	claims := jwt.MapClaims{
		"teamId": foundTeam.ID,
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

	// Start a new session for the team
	session, err := h.sessionService.StartSession(foundTeam.ID, 64, "normal")
	if err != nil {
		http.Error(w, "Failed to start session", http.StatusInternalServerError)
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
			ID:    foundTeam.ID,
			Name:  foundTeam.TeamName,
			Color: foundTeam.TeamColor,
		},
		SessionID: session.SessionID,
	})
}

// GetTeamProgressHandler returns the private progress for the authenticated team
func (h *ClientHandler) GetTeamProgressHandler(w http.ResponseWriter, r *http.Request) {
	teamID, ok := r.Context().Value(middleware.TeamIDKey).(string)
	if !ok {
		http.Error(w, "Team ID not found in token", http.StatusUnauthorized)
		return
	}

	mu.RLock()
	team, ok := teams[teamID]
	mu.RUnlock()

	if !ok {
		http.Error(w, "Team not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(team)
}

// GetLeaderboardHandler returns the public leaderboard
func (h *ClientHandler) GetLeaderboardHandler(w http.ResponseWriter, r *http.Request) {
	entries := h.leaderboardRepo.GetLeaderboard()

	response := map[string]interface{}{
		"entries": entries,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetBoardHandler returns the game board for a given session
func (h *ClientHandler) GetBoardHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionId")

	session, err := h.sessionService.GetSession(sessionID)
	if err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Build trait definitions per docs: traitKey, type, values
	traitDefs := h.traitCatalog.GetAllTraits()
	defs := make([]map[string]interface{}, 0, len(traitDefs))
	for _, td := range traitDefs {
		def := map[string]interface{}{
			"traitKey": td.TraitKey,
			"type":     td.Type,
		}
		if td.Values != nil && len(td.Values) > 0 {
			def["values"] = td.Values
		}
		defs = append(defs, def)
	}

	response := map[string]interface{}{
		"sessionId":        session.SessionID,
		"candidates":       session.Candidates,
		"traitDefinitions": defs,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}