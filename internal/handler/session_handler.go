package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/guesswho/internal/service"
)

// SessionHandler handles session-related HTTP requests
type SessionHandler struct {
	sessionService service.SessionService
	traitCatalog   service.TraitCatalogService
}

// NewSessionHandler creates a new session handler
func NewSessionHandler(sessionService service.SessionService, traitCatalog service.TraitCatalogService) *SessionHandler {
	return &SessionHandler{
		sessionService: sessionService,
		traitCatalog:   traitCatalog,
	}
}

// StartSessionRequest represents the request to start a new session
type StartSessionRequest struct {
	BoardSize  int    `json:"boardSize"`
	Difficulty string `json:"difficulty"`
}

// StartSessionResponse represents the response when starting a session
type StartSessionResponse struct {
	SessionID       string      `json:"sessionId"`
	BoardSize       int         `json:"boardSize"`
	TraitsAvailable int         `json:"traitsAvailable"`
	GuessLimit      int         `json:"guessLimit"`
	ChaosProfile    interface{} `json:"chaosProfile"`
}

// StartSession handles POST /sessions/start
func (h *SessionHandler) StartSession(w http.ResponseWriter, r *http.Request) {
	teamID := r.Header.Get("X-Team-Id")
	if teamID == "" {
		http.Error(w, "X-Team-Id header required", http.StatusBadRequest)
		return
	}

	var req StartSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, err := h.sessionService.StartSession(teamID, req.BoardSize, req.Difficulty)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := StartSessionResponse{
		SessionID:       session.SessionID,
		BoardSize:       session.BoardSize,
		TraitsAvailable: session.TraitsAvailable,
		GuessLimit:      session.GuessLimit,
		ChaosProfile:    session.ChaosProfile,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetBoard handles GET /sessions/{sessionId}/board
func (h *SessionHandler) GetBoard(w http.ResponseWriter, r *http.Request) {
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

// AskQuestionRequest represents a request to ask a question
type AskQuestionRequest struct {
	QuestionID string `json:"questionId"`
}

// AskQuestion handles POST /sessions/{sessionId}/ask
func (h *SessionHandler) AskQuestion(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionId")

	var req AskQuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	answer, err := h.sessionService.AskQuestion(sessionID, req.QuestionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(answer)
}

// GuessRequest represents a guess submission
type GuessRequest struct {
	CandidateID string                   `json:"candidateId"`
	Evidence    []map[string]interface{} `json:"evidence,omitempty"`
}

// SubmitGuess handles POST /sessions/{sessionId}/guess
func (h *SessionHandler) SubmitGuess(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionId")

	var req GuessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	result, err := h.sessionService.SubmitGuess(sessionID, req.CandidateID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Fetch session for additional fields
	session, err := h.sessionService.GetSession(sessionID)
	if err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if result.Correct {
		response := map[string]interface{}{
			"correct":          true,
			"questionsAsked":   session.GetQuestionsAskedCount(),
			"timeElapsed":      time.Since(session.CreatedAt).Seconds(),
			"score":            result.Score,
			"sessionEnded":     session.Completed,
			"guessesRemaining": session.GuessesRemaining,
			"guessedCount":     session.GetGuessedCount(),
		}
		json.NewEncoder(w).Encode(response)
		return
	}

	// Failure response with dynamic session state
	response := map[string]interface{}{
		"correct":          false,
		"penalty":          -500,
		"sessionEnded":     session.Completed,
		"guessesRemaining": session.GuessesRemaining,
		"guessedCount":     session.GetGuessedCount(),
	}
	json.NewEncoder(w).Encode(response)
}

// Status handles GET /sessions/{sessionId}/status
func (h *SessionHandler) Status(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionId")

	session, err := h.sessionService.GetSession(sessionID)
	if err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	response := map[string]interface{}{
		"sessionId":        session.SessionID,
		"active":           !session.Completed,
		"questionsAsked":   session.GetQuestionsAskedCount(),
		"guessesRemaining": session.GuessesRemaining,
		"startTime":        session.CreatedAt.Format(time.RFC3339),
		"elapsedSeconds":   int(time.Since(session.CreatedAt).Seconds()),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Reveal handles POST /sessions/{sessionId}/reveal
func (h *SessionHandler) Reveal(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionId")

	res, err := h.sessionService.Reveal(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}
