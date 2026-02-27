package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/guesswho/internal/logging"
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
		logging.Warn(r.Context(), "missing X-Team-Id header")
		http.Error(w, "X-Team-Id header required", http.StatusBadRequest)
		return
	}

	session, err := h.sessionService.StartSession(r.Context(), teamID)
	if err != nil {
		logging.Error(r.Context(), "failed to start session", "error", err)
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

	session, err := h.sessionService.GetSession(r.Context(), sessionID)
	if err != nil {
		logging.Warn(r.Context(), "session not found for board request", "error", err)
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
		if len(td.Values) > 0 {
			def["values"] = td.Values
		}
		defs = append(defs, def)
	}

	// Build the board: return the trait map for each position in the
	// session's stored shuffled order, including the per-session fake
	// candidateId. Never expose the real candidateId, name, or imagePath.
	board := make([]map[string]interface{}, 0, len(session.Candidates))
	for _, c := range session.Candidates {
		entry := map[string]interface{}{
			"traits": c.Traits,
		}
		if fakeID, ok := session.CandidateIDMapRev[c.CandidateID]; ok {
			entry["candidateId"] = fakeID
		}
		board = append(board, entry)
	}

	response := map[string]interface{}{
		"sessionId":        session.SessionID,
		"board":            board,
		"traitDefinitions": defs,
	}

	logging.Debug(r.Context(), "board retrieved", "boardSize", len(session.Candidates))

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
		logging.Warn(r.Context(), "invalid request body for ask", "error", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	answer, err := h.sessionService.AskQuestion(r.Context(), sessionID, req.QuestionID)
	if err != nil {
		logging.Error(r.Context(), "ask question failed", "questionId", req.QuestionID, "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	logging.Info(r.Context(), "question answered", "questionId", req.QuestionID)

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
		logging.Warn(r.Context(), "invalid request body for guess", "error", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	result, err := h.sessionService.SubmitGuess(r.Context(), sessionID, req.CandidateID)
	if err != nil {
		logging.Error(r.Context(), "submit guess failed", "candidateId", req.CandidateID, "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Fetch session for additional fields
	session, err := h.sessionService.GetSession(r.Context(), sessionID)
	if err != nil {
		logging.Error(r.Context(), "session not found after guess", "error", err)
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if result.Correct {
		logging.Info(r.Context(), "correct guess", "score", result.Score)
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

	logging.Info(r.Context(), "incorrect guess", "guessesRemaining", session.GuessesRemaining)

	// Failure response with dynamic session state
	response := map[string]interface{}{
		"correct":          false,
		"penalty":          -200,
		"sessionEnded":     session.Completed,
		"guessesRemaining": session.GuessesRemaining,
		"guessedCount":     session.GetGuessedCount(),
	}
	json.NewEncoder(w).Encode(response)
}

// Status handles GET /sessions/{sessionId}/status
func (h *SessionHandler) Status(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionId")

	session, err := h.sessionService.GetSession(r.Context(), sessionID)
	if err != nil {
		logging.Warn(r.Context(), "session not found for status", "error", err)
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

	res, err := h.sessionService.Reveal(r.Context(), sessionID)
	if err != nil {
		logging.Error(r.Context(), "reveal failed", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	logging.Info(r.Context(), "session revealed")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}
