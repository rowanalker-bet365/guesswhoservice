package handler

import (
	"encoding/json"
	"errors"
	"math/rand"
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
	SessionID       string `json:"sessionId"`
	BoardSize       int    `json:"boardSize"`
	TraitsAvailable int    `json:"traitsAvailable"`
	GuessLimit      int    `json:"guessLimit"`
}

// StartSession handles POST /sessions/start
func (h *SessionHandler) StartSession(w http.ResponseWriter, r *http.Request) {
	teamID := r.Header.Get("X-Team-Id")
	if teamID == "" {
		logging.Warn(r.Context(), "missing X-Team-Id header")
		writeErrorJSON(w, http.StatusBadRequest, APIError{
			Error:   "missing_header",
			Message: "The X-Team-Id header is required to start a session. Include your team ID in the request header: X-Team-Id: your-team-id",
			Field:   "X-Team-Id",
		})
		return
	}

	session, err := h.sessionService.StartSession(r.Context(), teamID)
	if err != nil {
		if errors.Is(err, service.ErrTooManySessions) {
			writeErrorJSON(w, http.StatusTooManyRequests, APIError{
				Error:   "too_many_sessions",
				Message: "Your team already has the maximum number of active sessions. Complete or reveal an existing session before starting a new one.",
			})
			return
		}
		if errors.Is(err, service.ErrTeamNotFound) {
			writeErrorJSON(w, http.StatusForbidden, APIError{
				Error:   "team_not_found",
				Message: "The team ID provided does not correspond to a registered team.",
				Field:   "X-Team-Id",
			})
			return
		}
		logging.Error(r.Context(), "failed to start session", "error", err)
		writeErrorJSON(w, http.StatusInternalServerError, APIError{
			Error:   "session_creation_failed",
			Message: "The server was unable to create a new session. Please retry your request. If the problem persists, contact the event organiser.",
		})
		return
	}

	response := StartSessionResponse{
		SessionID:       session.SessionID,
		BoardSize:       session.BoardSize,
		TraitsAvailable: session.TraitsAvailable,
		GuessLimit:      session.GuessLimit,
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
		writeErrorJSON(w, http.StatusNotFound, APIError{
			Error:     "session_not_found",
			Message:   "No session exists with ID '" + sessionID + "'. The session may have expired or the ID is incorrect. Start a new session with POST /sessions/start.",
			SessionID: sessionID,
		})
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

	// Build the board: return each candidate's trait values filtered to only
	// those in the initial visible subset or revealed via questions.
	board := make([]map[string]interface{}, 0, len(session.Candidates))
	for _, c := range session.Candidates {
		entry := map[string]interface{}{}
		if fakeID, ok := session.CandidateIDMapRev[c.CandidateID]; ok {
			entry["candidateId"] = fakeID
		} else {
			entry["candidateId"] = c.CandidateID
		}
		// Only include traits in this candidate's initial subset or revealed session-wide via questions
		visibleTraits := map[string]interface{}{}
		for traitName, traitValue := range c.Traits {
			if session.IsTraitVisibleForCandidate(c.CandidateID, traitName) {
				visibleTraits[traitName] = traitValue
			}
		}
		entry["traits"] = visibleTraits
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
	// Value is the specific value being asked about for enum traits.
	// For boolean traits this field is ignored.
	// For enum traits it is required; the response will be true if the
	// target's trait value matches this value (case-insensitive).
	Value string `json:"value,omitempty"`
}

// AskQuestion handles POST /sessions/{sessionId}/ask
func (h *SessionHandler) AskQuestion(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionId")

	var req AskQuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logging.Warn(r.Context(), "invalid request body for ask", "error", err)
		writeErrorJSON(w, http.StatusBadRequest, APIError{
			Error:   "invalid_request_body",
			Message: "The request body could not be parsed as JSON. Expected format: {\"questionId\": \"T04\"} Ensure Content-Type is application/json.",
		})
		return
	}

	answer, err := h.sessionService.AskQuestion(r.Context(), sessionID, req.QuestionID, req.Value)
	if err != nil {
		// Check for chaos-injected error first
		var chaosErr *service.ChaosError
		if errors.As(err, &chaosErr) {
			logging.Info(r.Context(), "chaos: injected failure on ask", "questionId", req.QuestionID)
			logging.MarkChaosInjected(r)
			writeErrorJSON(w, chaosErr.StatusCode, APIError{
				Error:   chaosErr.ErrorCode,
				Message: chaosErr.Message,
			})
			return
		}
		logging.Error(r.Context(), "ask question failed", "questionId", req.QuestionID, "error", err)
		statusCode, apiErr := classifyServiceError(err, sessionID, req.QuestionID)
		writeErrorJSON(w, statusCode, apiErr)
		return
	}

	logging.Info(r.Context(), "question answered", "questionId", req.QuestionID)

	// Add timing jitter to prevent timing-based inference
	jitter := time.Duration(rand.Intn(151)+50) * time.Millisecond
	time.Sleep(jitter)

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
		writeErrorJSON(w, http.StatusBadRequest, APIError{
			Error:   "invalid_request_body",
			Message: "The request body could not be parsed as JSON. Expected format: {\"candidateId\": \"P01\"}. Ensure Content-Type is application/json.",
		})
		return
	}

	result, err := h.sessionService.SubmitGuess(r.Context(), sessionID, req.CandidateID)
	if err != nil {
		var chaosErr *service.ChaosError
		if errors.As(err, &chaosErr) {
			logging.Info(r.Context(), "chaos: injected failure on guess", "candidateId", req.CandidateID)
			logging.MarkChaosInjected(r)
			writeErrorJSON(w, chaosErr.StatusCode, APIError{
				Error:   chaosErr.ErrorCode,
				Message: chaosErr.Message,
			})
			return
		}
		logging.Error(r.Context(), "submit guess failed", "candidateId", req.CandidateID, "error", err)
		statusCode, apiErr := classifyServiceError(err, sessionID, req.CandidateID)
		writeErrorJSON(w, statusCode, apiErr)
		return
	}

	// Fetch session for additional fields
	session, err := h.sessionService.GetSession(r.Context(), sessionID)
	if err != nil {
		logging.Error(r.Context(), "session not found after guess", "error", err)
		writeErrorJSON(w, http.StatusInternalServerError, APIError{
			Error:     "internal_error",
			Message:   "The session could not be retrieved after processing your guess. This is an internal error. Please retry.",
			SessionID: sessionID,
		})
		return
	}

	// Stage 2: encrypted guess response pending
	if session.PendingDecryption {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"pending":              true,
			"encryptedCharacterId": session.PendingEncryptedResponse,
			"cipher":               session.EncryptCipher,
			"encoding":             "hex",
			"hint":                 "Derive the Witness Key from your unencrypted trait answers using HKDF-SHA256 (salt=sessionId, info=\"guesswho-witness-v1\"), then decrypt to reveal the character ID",
		})
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
		writeErrorJSON(w, http.StatusNotFound, APIError{
			Error:     "session_not_found",
			Message:   "No session exists with ID '" + sessionID + "'. The session may have expired or the ID is incorrect. Start a new session with POST /sessions/start.",
			SessionID: sessionID,
		})
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

// DecryptRequest is the request body for POST /sessions/{sessionId}/decrypt
type DecryptRequest struct {
	CandidateID string `json:"candidateId"`
	WitnessKey  string `json:"witnessKey"`
}

// SubmitDecryption handles POST /sessions/{sessionId}/decrypt
func (h *SessionHandler) SubmitDecryption(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionId")
	teamID := r.Header.Get("X-Team-Id")
	if teamID == "" {
		writeErrorJSON(w, http.StatusBadRequest, APIError{
			Error:   "missing_header",
			Message: "The X-Team-Id header is required.",
			Field:   "X-Team-Id",
		})
		return
	}

	var req DecryptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CandidateID == "" || req.WitnessKey == "" {
		writeErrorJSON(w, http.StatusBadRequest, APIError{
			Error:     "invalid_request",
			Message:   "A JSON body with \"candidateId\" and \"witnessKey\" is required. Example: {\"candidateId\": \"P01\", \"witnessKey\": \"<hex-encoded-key>\"}.",
			SessionID: sessionID,
		})
		return
	}

	result, err := h.sessionService.SubmitDecryption(r.Context(), sessionID, teamID, req.CandidateID, req.WitnessKey)
	if err != nil {
		statusCode, apiErr := classifyServiceError(err, sessionID)
		writeErrorJSON(w, statusCode, apiErr)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if result.Correct {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"correct":   true,
			"score":     result.Score,
			"breakdown": result.Breakdown,
			"stats":     result.Stats,
			"message":   "Cipher Master! You decrypted the response and identified the character.",
		})
	} else {
		// Fetch session to include current guessesRemaining in the response
		session, sessionErr := h.sessionService.GetSession(r.Context(), sessionID)
		guessesRemaining := 0
		if sessionErr == nil {
			guessesRemaining = session.GuessesRemaining
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"correct":          false,
			"guessesRemaining": guessesRemaining,
			"message":          "Decryption accepted but your original guess was wrong. −200 penalty applied.",
		})
	}
}

// Reveal handles POST /sessions/{sessionId}/reveal
func (h *SessionHandler) Reveal(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionId")

	res, err := h.sessionService.Reveal(r.Context(), sessionID)
	if err != nil {
		var chaosErr *service.ChaosError
		if errors.As(err, &chaosErr) {
			logging.Info(r.Context(), "chaos: injected failure on reveal")
			logging.MarkChaosInjected(r)
			writeErrorJSON(w, chaosErr.StatusCode, APIError{
				Error:   chaosErr.ErrorCode,
				Message: chaosErr.Message,
			})
			return
		}
		logging.Error(r.Context(), "reveal failed", "error", err)
		statusCode, apiErr := classifyServiceError(err, sessionID)
		writeErrorJSON(w, statusCode, apiErr)
		return
	}

	logging.Info(r.Context(), "session revealed")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}
