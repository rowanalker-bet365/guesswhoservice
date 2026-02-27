package service

import (
	"context"
	"fmt"
	"hash/fnv"
	"math/rand"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/guesswho/internal/db"
	"github.com/guesswho/internal/domain"
	"github.com/guesswho/internal/logging"
)

// SessionService manages game sessions
type SessionService interface {
	StartSession(ctx context.Context, teamID string) (*domain.Session, error)
	GetSession(ctx context.Context, sessionID string) (*domain.Session, error)
	AskQuestion(ctx context.Context, sessionID string, questionID string) (*domain.TraitAnswer, error)
	SubmitGuess(ctx context.Context, sessionID string, candidateID string) (*domain.GuessResult, error)
	Reveal(ctx context.Context, sessionID string) (*domain.RevealResult, error)
}

type sessionService struct {
	dbStore           *db.Store
	traitCatalog      TraitCatalogService
	characterCatalog  CharacterCatalogService
	boardGenerator    BoardGeneratorService
	encryptionService EncryptionService
	chaosService      ChaosService
	scoringService    ScoringService
	milestoneService  MilestoneService
	chaosInterval     int
	chaosWindow       int
}

// SessionServiceConfig holds configuration for the session service
type SessionServiceConfig struct {
	ChaosInterval int
	ChaosWindow   int
}

// NewSessionService creates a new session service
func NewSessionService(
	dbStore *db.Store,
	traitCatalog TraitCatalogService,
	characterCatalog CharacterCatalogService,
	boardGenerator BoardGeneratorService,
	encryptionService EncryptionService,
	chaosService ChaosService,
	scoringService ScoringService,
	milestoneService MilestoneService,
	config SessionServiceConfig,
) SessionService {
	return &sessionService{
		dbStore:           dbStore,
		traitCatalog:      traitCatalog,
		characterCatalog:  characterCatalog,
		boardGenerator:    boardGenerator,
		encryptionService: encryptionService,
		chaosService:      chaosService,
		scoringService:    scoringService,
		milestoneService:  milestoneService,
		chaosInterval:     config.ChaosInterval,
		chaosWindow:       config.ChaosWindow,
	}
}

// sessionShuffleSeed derives a deterministic per-session seed from the sessionID.
func sessionShuffleSeed(sessionID string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(sessionID))
	return int64(h.Sum64())
}

func (s *sessionService) StartSession(ctx context.Context, teamID string) (*domain.Session, error) {
	sessionID := fmt.Sprintf("s_%s", uuid.New().String()[:8])

	// Derive a unique seed from the session ID so that every session gets a
	// different board order and target character.
	perSessionSeed := sessionShuffleSeed(sessionID)

	chaosProfile := domain.ChaosProfile{
		Mode:            domain.ChaosModeScheduled,
		WindowSeconds:   s.chaosWindow,
		IntervalSeconds: s.chaosInterval,
	}

	session := domain.NewSession(sessionID, teamID, 3, perSessionSeed, chaosProfile)

	// Generate board: load all characters from the catalog and shuffle them
	// using the per-session seed so every session has a unique board order.
	candidates, err := s.boardGenerator.GenerateBoard(perSessionSeed, s.characterCatalog)
	if err != nil {
		return nil, fmt.Errorf("failed to generate board: %w", err)
	}

	// Retrieve team data to filter solved characters
	teamData, err := s.dbStore.ReadTeamData(ctx, teamID)
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to read team data: %w", err)
	}
	if err == redis.Nil {
		// Should not happen if team is registered, but handle gracefully
		teamData = &db.TeamData{}
	}

	// Filter out solved candidates
	solvedSet := make(map[string]bool)
	for _, id := range teamData.SolvedCharacters {
		solvedSet[id] = true
	}

	var availableCandidates []*domain.Candidate
	for _, c := range candidates {
		if !solvedSet[c.CandidateID] {
			availableCandidates = append(availableCandidates, c)
		}
	}

	// Track whether a full reset occurred before overwriting availableCandidates.
	resetOccurred := len(availableCandidates) == 0

	// If all candidates are solved, reset the solved list and use all candidates
	if resetOccurred {
		availableCandidates = candidates
	}

	// GenerateBoard already shuffled the candidates with perSessionSeed, so
	// session.Candidates is already in a unique per-session order.
	session.Candidates = availableCandidates

	// Generate per-session fake ID mapping for anti-cheat.
	// Fake IDs (P01, P02, ...) correspond to the shuffled board positions.
	candidateIDMap := make(map[string]string, len(availableCandidates))
	candidateIDMapRev := make(map[string]string, len(availableCandidates))
	for i, c := range availableCandidates {
		fakeID := fmt.Sprintf("P%02d", i+1)
		candidateIDMap[fakeID] = c.CandidateID
		candidateIDMapRev[c.CandidateID] = fakeID
	}
	session.CandidateIDMap = candidateIDMap
	session.CandidateIDMapRev = candidateIDMapRev

	// Select a random target using the per-session seed (SelectTarget uses
	// seed+1000 internally so it does not collide with the board shuffle).
	session.TargetCandidate = s.boardGenerator.SelectTarget(availableCandidates, perSessionSeed)

	if session.TargetCandidate == nil {
		return nil, fmt.Errorf("failed to select target candidate")
	}

	// Save session
	if err := s.dbStore.WriteSession(ctx, session); err != nil {
		return nil, fmt.Errorf("failed to save session: %w", err)
	}

	// If all characters were solved, clear the solved set in Redis so the team starts fresh.
	if resetOccurred {
		if err := s.dbStore.ClearSolvedCharacters(ctx, teamID); err != nil {
			logging.Warn(ctx, "failed to clear solved characters", "error", err)
		}
	}

	// Set the active session ID using a targeted HSET — do NOT use WriteTeamData here,
	// as that would overwrite score/solves/solved-characters with the stale snapshot.
	if err := s.dbStore.SetActiveSession(ctx, teamID, sessionID); err != nil {
		return nil, fmt.Errorf("failed to update active session for team %s: %w", teamID, err)
	}

	// Publish a game update so SSE clients are notified of the new active session.
	if err := s.dbStore.PublishGameUpdate(ctx, teamID, ""); err != nil {
		logging.Error(ctx, "failed to publish game update on session start", "error", err)
	}

	// M1: First Round Started — awarded once when a team starts their first session.
	s.milestoneService.AwardIfAbsent(ctx, teamID, domain.MilestoneM1)

	logging.Info(ctx, "session started", "sessionId", sessionID, "boardSize", len(availableCandidates), "targetCandidate", session.TargetCandidate.CandidateID)

	return session, nil
}

func (s *sessionService) GetSession(ctx context.Context, sessionID string) (*domain.Session, error) {
	return s.dbStore.ReadSession(ctx, sessionID)
}

func (s *sessionService) AskQuestion(ctx context.Context, sessionID string, questionID string) (*domain.TraitAnswer, error) {
	logging.Debug(ctx, "processing question", "questionId", questionID)

	session, err := s.dbStore.ReadSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	if session.Completed {
		return nil, fmt.Errorf("session already completed")
	}

	// Get trait definition
	traitDef, exists := s.traitCatalog.GetTraitByID(questionID)
	if !exists {
		return nil, fmt.Errorf("invalid question ID: %s", questionID)
	}

	// Check for chaos/failure injection — returns a ChaosError (real HTTP error)
	if s.chaosService.ShouldFail(session) {
		session.IncrementFailure("failed")
		s.dbStore.WriteSession(ctx, session)

		logging.Warn(ctx, "chaos: injecting failure", "questionId", questionID)
		return nil, s.chaosService.GetChaosError()
	}

	// Get the answer from the target candidate
	answer, exists := session.TargetCandidate.GetTrait(traitDef.TraitKey)
	if !exists {
		return nil, fmt.Errorf("trait not found on target: %s", traitDef.TraitKey)
	}

	// Record the question
	session.RecordQuestion(questionID)
	s.dbStore.WriteSession(ctx, session)

	// M2: First Successful Question — awarded after the first non-degraded answer.
	s.milestoneService.AwardIfAbsent(ctx, session.TeamID, domain.MilestoneM2)

	// M3: Elimination Working — awarded when ≥ 3 questions have been asked in a session.
	if session.GetQuestionsAskedCount() >= 3 {
		s.milestoneService.AwardIfAbsent(ctx, session.TeamID, domain.MilestoneM3)
	}

	// S3: Resilience — awarded when a successful answer is received during an active chaos window.
	if s.chaosService.IsInChaosWindow(session) {
		s.milestoneService.AwardIfAbsent(ctx, session.TeamID, domain.MilestoneS3)
	}

	// Determine if this response should be encrypted.
	// Decision is deterministic per questionID per session — once decided, it never changes.
	shouldEncrypt := false
	if decided, exists := session.EncryptedQuestions[questionID]; exists {
		// Already decided for this question in this session
		shouldEncrypt = decided
	} else {
		// First question in the session (before RecordQuestion was called above) is never encrypted.
		// QuestionsAsked already includes this question (recorded above), so count > 1 means not first.
		if len(session.QuestionsAsked) > 1 {
			roll := rand.Intn(100) + 1 // 1-100
			shouldEncrypt = roll > 60   // ~40% chance of encryption
		}
		if session.EncryptedQuestions == nil {
			session.EncryptedQuestions = make(map[string]bool)
		}
		session.EncryptedQuestions[questionID] = shouldEncrypt
		// Persist the decision
		s.dbStore.WriteSession(ctx, session)
	}

	// Build response
	traitAnswer := &domain.TraitAnswer{
		QuestionID: questionID,
		TraitKey:   traitDef.TraitKey,
	}

	if shouldEncrypt {
		plaintext := fmt.Sprintf("%v", answer)
		encrypted, err := s.encryptionService.Encrypt(plaintext, session.EncryptCipher, session.EncryptKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt answer: %w", err)
		}
		traitAnswer.Answer = encrypted
		traitAnswer.Status = "encrypted"
	} else {
		traitAnswer.Answer = answer
	}

	logging.Debug(ctx, "question answered", "questionId", questionID, "cipher", session.EncryptCipher)

	return traitAnswer, nil
}

func (s *sessionService) SubmitGuess(ctx context.Context, sessionID string, candidateID string) (*domain.GuessResult, error) {
	session, err := s.dbStore.ReadSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	if session.Completed {
		return nil, fmt.Errorf("session already completed")
	}

	// Chaos injection — can fail any session API during chaos windows
	if s.chaosService.ShouldFail(session) {
		session.IncrementFailure("failed")
		s.dbStore.WriteSession(ctx, session)
		logging.Warn(ctx, "chaos: injecting failure on guess", "candidateId", candidateID)
		return nil, s.chaosService.GetChaosError()
	}

	// If the player has used all allowed failed guesses, block further guesses
	if session.GuessesRemaining <= 0 {
		return nil, fmt.Errorf("no guesses remaining")
	}

	// Resolve fake candidate ID to real ID
	realCandidateID, exists := session.CandidateIDMap[candidateID]
	if !exists {
		// Backward compatibility: check if it's a raw real ID
		for _, c := range session.Candidates {
			if c.CandidateID == candidateID {
				realCandidateID = candidateID
				exists = true
				logging.Warn(ctx, "DEPRECATED: guess submitted with real candidate ID", "candidateId", candidateID)
				break
			}
		}
	}
	if !exists {
		return nil, fmt.Errorf("invalid candidate ID: %s", candidateID)
	}

	// Check if guess is correct
	correct := session.TargetCandidate.CandidateID == realCandidateID

	// Calculate per-guess score
	var breakdown domain.ScoreBreakdown
	var totalScore int

	if correct {
		breakdown = s.scoringService.CalculateScore(session)
		totalScore = breakdown.Base + breakdown.TimeBonus + breakdown.QuestionBonus - breakdown.ReliabilityPenalty
		if totalScore < 0 {
			totalScore = 0
		}
	} else {
		// Wrong guess penalty (per guess)
		breakdown = domain.ScoreBreakdown{
			Base:               0,
			TimeBonus:          0,
			QuestionBonus:      0,
			ReliabilityPenalty: 200,
		}
		totalScore = -200
	}

	// Update session state
	//  - Track unique candidates guessed
	//  - Only decrement failed guesses on wrong guesses
	session.RecordGuess(realCandidateID)
	if correct {
		session.CorrectGuess = true
		// Mark session as complete immediately on correct guess
		session.MarkComplete(true)

		// M4: First Correct Solve — awarded on the first correct guess.
		s.milestoneService.AwardIfAbsent(ctx, session.TeamID, domain.MilestoneM4)

		// S1: Efficiency — awarded when a character is solved with ≤ 10 questions.
		if session.GetQuestionsAskedCount() <= 10 {
			s.milestoneService.AwardIfAbsent(ctx, session.TeamID, domain.MilestoneS1)
		}
	} else {
		// Wrong guess: apply -200 penalty immediately, regardless of whether the session ends.
		if err := s.dbStore.IncrementTeamScore(ctx, session.TeamID, -200); err != nil {
			logging.Error(ctx, "failed to apply wrong-guess penalty", "error", err)
		}
		session.DecrementGuess()
	}

	// End session if:
	//  - All candidates have been guessed, or
	//  - Allowed failed guesses are exhausted
	if !session.Completed && (session.GuessesRemaining <= 0 || session.GetGuessedCount() >= len(session.Candidates)) {
		session.MarkComplete(false)
	}

	// Persist updates
	_ = s.dbStore.WriteSession(ctx, session)

	// Record on leaderboard only when the session actually ends
	if session.Completed {
		finalCorrect := session.CorrectGuess
		// Reuse the score already computed above to avoid a second CalculateScore call
		// (calculateTimeBonus uses time.Since, so two calls return different values).
		finalScore := 0
		if finalCorrect {
			finalScore = totalScore
		}

		// Update team stats including solved characters and clearing active session
		s.updateTeamStats(ctx, session, finalScore, finalCorrect, realCandidateID)

		// S2: Automation — awarded once the team reaches 3+ total correct solves.
		// Read the updated solve count after updateTeamStats has incremented it.
		if finalCorrect {
			if teamData, err := s.dbStore.ReadTeamData(ctx, session.TeamID); err == nil && teamData.Solves >= 3 {
				s.milestoneService.AwardIfAbsent(ctx, session.TeamID, domain.MilestoneS2)
			}
		}
	}

	// Publish an update now that all data is persisted, regardless of guess correctness.
	if correct {
		// For a correct guess, include the solved character ID.
		if err := s.dbStore.PublishGameUpdate(ctx, session.TeamID, realCandidateID); err != nil {
			logging.Error(ctx, "failed to publish game update", "error", err)
		}
	} else {
		// For an incorrect guess, send a generic update to trigger a client refetch.
		if err := s.dbStore.PublishGameUpdate(ctx, session.TeamID, ""); err != nil {
			logging.Error(ctx, "failed to publish game update", "error", err)
		}
	}

	logging.Info(ctx, "guess submitted", "fakeId", candidateID, "realId", realCandidateID, "correct", correct, "score", totalScore, "guessesRemaining", session.GuessesRemaining)

	// Build per-guess result
	result := &domain.GuessResult{
		Correct:   correct,
		Score:     totalScore,
		Breakdown: breakdown,
		Stats: domain.SessionStats{
			TimeSeconds:    session.GetElapsedTime(),
			QuestionsAsked: session.GetQuestionsAskedCount(),
			FailedRequests: session.FailedRequests,
		},
	}

	return result, nil
}

func (s *sessionService) Reveal(ctx context.Context, sessionID string) (*domain.RevealResult, error) {
	session, err := s.dbStore.ReadSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// Chaos injection
	if s.chaosService.ShouldFail(session) {
		session.IncrementFailure("failed")
		s.dbStore.WriteSession(ctx, session)
		logging.Warn(ctx, "chaos: injecting failure on reveal")
		return nil, s.chaosService.GetChaosError()
	}

	alreadyCompleted := session.Completed

	// If not already completed, mark the session as completed.
	// Preserve whether the session had a correct guess previously.
	if !alreadyCompleted {
		session.MarkComplete(session.CorrectGuess)
		_ = s.dbStore.WriteSession(ctx, session)

		// Clear active session ID for the team using a targeted HSET to avoid
		// clobbering concurrent Lua script updates to score/solves.
		_ = s.dbStore.ClearActiveSession(ctx, session.TeamID)
	}

	logging.Info(ctx, "session revealed", "wasCompleted", alreadyCompleted)

	// Map the real candidate ID to the fake one so we never leak the real ID.
	fakeID := session.TargetCandidate.CandidateID
	if mapped, ok := session.CandidateIDMapRev[session.TargetCandidate.CandidateID]; ok {
		fakeID = mapped
	}
	revealTarget := &domain.Candidate{
		CandidateID: fakeID,
		Traits:      session.TargetCandidate.Traits,
	}

	res := &domain.RevealResult{
		Target: revealTarget,
		Stats: domain.SessionStats{
			TimeSeconds:    session.GetElapsedTime(),
			QuestionsAsked: session.GetQuestionsAskedCount(),
			FailedRequests: session.FailedRequests,
		},
		SessionEnded: session.Completed,
	}
	return res, nil
}

func (s *sessionService) updateTeamStats(ctx context.Context, session *domain.Session, score int, correct bool, solvedCandidateID string) {
	if correct && solvedCandidateID != "" {
		logging.Info(ctx, fmt.Sprintf("[MASTERBOARD] Calling UpdateTeamStatsAtomic: team=%s char=%s score=%d", session.TeamID, solvedCandidateID, score))
		if err := s.dbStore.UpdateTeamStatsAtomic(ctx, session.TeamID, solvedCandidateID, score); err != nil {
			logging.Error(ctx, "Error in atomic team stats update for team", "error", err)
			return
		}
		logging.Info(ctx, fmt.Sprintf("[MASTERBOARD] Atomic update succeeded: team=%s char=%s", session.TeamID, solvedCandidateID))

		// Conditionally update fastest solve time (atomic compare-and-set via Lua).
		elapsed := time.Since(session.CreatedAt)
		if err := s.dbStore.UpdateFastestSolve(ctx, session.TeamID, elapsed); err != nil {
			logging.Error(ctx, "failed to update fastest solve", "error", err)
		}
	}
	// Wrong-guess penalties are applied immediately in SubmitGuess, not here.

	// Clear the active session ID so the team can start a new session.
	if err := s.dbStore.ClearActiveSession(ctx, session.TeamID); err != nil {
		logging.Error(ctx, "failed to clear active session", "error", err)
	}
}
