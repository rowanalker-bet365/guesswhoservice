package service

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"math/rand"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/guesswho/internal/db"
	"github.com/guesswho/internal/domain"
)


// SessionService manages game sessions
type SessionService interface {
	StartSession(teamID string, boardSize int, difficulty string) (*domain.Session, error)
	GetSession(sessionID string) (*domain.Session, error)
	AskQuestion(sessionID string, questionID string) (*domain.TraitAnswer, error)
	SubmitGuess(sessionID string, candidateID string) (*domain.GuessResult, error)
	Reveal(sessionID string) (*domain.RevealResult, error)
}

type sessionService struct {
	dbStore           *db.Store
	traitCatalog      TraitCatalogService
	boardGenerator    BoardGeneratorService
	encryptionService EncryptionService
	chaosService      ChaosService
	scoringService    ScoringService
	chaosEnabled      bool
	chaosInterval     int
	chaosWindow       int
}

// SessionServiceConfig holds configuration for the session service
type SessionServiceConfig struct {
	ChaosEnabled  bool
	ChaosInterval int
	ChaosWindow   int
}

// NewSessionService creates a new session service
func NewSessionService(
	dbStore *db.Store,
	traitCatalog TraitCatalogService,
	boardGenerator BoardGeneratorService,
	encryptionService EncryptionService,
	chaosService ChaosService,
	scoringService ScoringService,
	config SessionServiceConfig,
) SessionService {
	return &sessionService{
		dbStore:           dbStore,
		traitCatalog:      traitCatalog,
		boardGenerator:    boardGenerator,
		encryptionService: encryptionService,
		chaosService:      chaosService,
		scoringService:    scoringService,
		chaosEnabled:      config.ChaosEnabled,
		chaosInterval:     config.ChaosInterval,
		chaosWindow:       config.ChaosWindow,
	}
}

// stableSeed returns a fixed seed so all sessions share the same board and target.
func stableSeed() int64 {
	// Adjust this constant if you want to rotate the global board.
	return 13371337
}

// sessionShuffleSeed derives a deterministic per-session seed from the sessionID.
func sessionShuffleSeed(sessionID string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(sessionID))
	return int64(h.Sum64())
}

func (s *sessionService) StartSession(teamID string, boardSize int, difficulty string) (*domain.Session, error) {
	if boardSize <= 0 {
		boardSize = 64
	}

	sessionID := fmt.Sprintf("s_%s", uuid.New().String()[:8])
	seed := stableSeed()

	chaosProfile := domain.ChaosProfile{
		Mode:            domain.ChaosModeScheduled,
		WindowSeconds:   s.chaosWindow,
		IntervalSeconds: s.chaosInterval,
	}

	session := domain.NewSession(sessionID, teamID, boardSize, 1, seed, chaosProfile)

	// Generate board
	candidates, err := s.boardGenerator.GenerateBoard(seed, boardSize, s.traitCatalog)
	if err != nil {
		return nil, fmt.Errorf("failed to generate board: %w", err)
	}

	// Retrieve team data to filter solved characters
	teamData, err := s.dbStore.ReadTeamData(context.Background(), teamID)
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

	// If all candidates are solved, reset the solved list and use all candidates
	if len(availableCandidates) == 0 {
		availableCandidates = candidates
		teamData.SolvedCharacters = []string{}
		// We will update the team data below with the new session ID, so the cleared list will be saved then.
	}

	session.Candidates = availableCandidates
	session.TargetCandidate = s.boardGenerator.SelectTarget(availableCandidates, seed)

	// Shuffle candidate presentation order per session while keeping the same board and target.
	{
		rng := rand.New(rand.NewSource(sessionShuffleSeed(sessionID)))
		rng.Shuffle(len(session.Candidates), func(i, j int) {
			session.Candidates[i], session.Candidates[j] = session.Candidates[j], session.Candidates[i]
		})
	}

	if session.TargetCandidate == nil {
		return nil, fmt.Errorf("failed to select target candidate")
	}

	// Save session
	if err := s.dbStore.WriteSession(context.Background(), session); err != nil {
		return nil, fmt.Errorf("failed to save session: %w", err)
	}

	// Update team's active session
	teamData.ActiveSessionID = sessionID
	if err := s.dbStore.WriteTeamData(context.Background(), teamID, teamData); err != nil {
		return nil, fmt.Errorf("failed to update team data: %w", err)
	}

	return session, nil
}

func (s *sessionService) GetSession(sessionID string) (*domain.Session, error) {
	return s.dbStore.ReadSession(context.Background(), sessionID)
}

func (s *sessionService) AskQuestion(sessionID string, questionID string) (*domain.TraitAnswer, error) {
	session, err := s.dbStore.ReadSession(context.Background(), sessionID)
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

	// Check for chaos/failure injection
	if s.chaosService.ShouldFail(session, traitDef) {
		session.IncrementFailure("failed")
		s.dbStore.WriteSession(context.Background(), session)

		return &domain.TraitAnswer{
			QuestionID: questionID,
			TraitKey:   traitDef.TraitKey,
			Status:     "degraded",
			RetryAfter: s.chaosService.GetRetryDelay(),
		}, nil
	}

	// Get the answer from the target candidate
	answer, exists := session.TargetCandidate.GetTrait(traitDef.TraitKey)
	if !exists {
		return nil, fmt.Errorf("trait not found on target: %s", traitDef.TraitKey)
	}

	// Record the question
	session.RecordQuestion(questionID)
	s.dbStore.WriteSession(context.Background(), session)

	// Build response
	traitAnswer := &domain.TraitAnswer{
		QuestionID: questionID,
		TraitKey:   traitDef.TraitKey,
	}

	// Encrypt if needed
	if traitDef.IsEncrypted {
		plaintext := fmt.Sprintf("%v", answer)
		encrypted, err := s.encryptionService.Encrypt(plaintext)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt answer: %w", err)
		}

		traitAnswer.Encrypted = encrypted
	} else {
		traitAnswer.Answer = answer
	}

	return traitAnswer, nil
}

func (s *sessionService) SubmitGuess(sessionID string, candidateID string) (*domain.GuessResult, error) {
	session, err := s.dbStore.ReadSession(context.Background(), sessionID)
	if err != nil {
		return nil, err
	}

	if session.Completed {
		return nil, fmt.Errorf("session already completed")
	}

	// If the player has used all allowed failed guesses, block further guesses
	if session.GuessesRemaining <= 0 {
		return nil, fmt.Errorf("no guesses remaining")
	}

	// Check if guess is correct
	correct := session.TargetCandidate.CandidateID == candidateID

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
			ReliabilityPenalty: 500,
		}
		totalScore = -500
	}

	// Update session state
	//  - Track unique candidates guessed
	//  - Only decrement failed guesses on wrong guesses
	session.RecordGuess(candidateID)
	if correct {
		session.CorrectGuess = true
		// Mark session as complete immediately on correct guess
		session.MarkComplete(true)
	} else {
		session.DecrementGuess()
	}

	// End session if:
	//  - All candidates have been guessed, or
	//  - Allowed failed guesses are exhausted
	if !session.Completed && (session.GuessesRemaining <= 0 || session.GetGuessedCount() >= len(session.Candidates)) {
		session.MarkComplete(false)
	}

	// Persist updates
	_ = s.dbStore.WriteSession(context.Background(), session)

	// Record on leaderboard only when the session actually ends
	if session.Completed {
		finalCorrect := session.CorrectGuess
		finalScore := 0
		if finalCorrect {
			fb := s.scoringService.CalculateScore(session)
			finalScore = fb.Base + fb.TimeBonus + fb.QuestionBonus - fb.ReliabilityPenalty
			if finalScore < 0 {
				finalScore = 0
			}
		} else {
			finalScore = -200
		}

		// Update team stats including solved characters and clearing active session
		s.updateTeamStats(session, finalScore, finalCorrect, candidateID)
	}

	// Publish an update now that all data is persisted, regardless of guess correctness.
	if correct {
		// For a correct guess, include the solved character ID.
		if err := s.dbStore.PublishGameUpdate(context.Background(), session.TeamID, candidateID); err != nil {
			log.Printf("Error publishing game update: %v", err)
		}
	} else {
		// For an incorrect guess, send a generic update to trigger a client refetch.
		if err := s.dbStore.PublishGameUpdate(context.Background(), session.TeamID, ""); err != nil {
			log.Printf("Error publishing game update: %v", err)
		}
	}

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

func (s *sessionService) Reveal(sessionID string) (*domain.RevealResult, error) {
	session, err := s.dbStore.ReadSession(context.Background(), sessionID)
	if err != nil {
		return nil, err
	}

	alreadyCompleted := session.Completed

	// If not already completed, mark the session as completed.
	// Preserve whether the session had a correct guess previously.
	if !alreadyCompleted {
		session.MarkComplete(session.CorrectGuess)
		_ = s.dbStore.WriteSession(context.Background(), session)

		// Clear active session ID for the team using a targeted HSET to avoid
		// clobbering concurrent Lua script updates to score/solves.
		_ = s.dbStore.ClearActiveSession(context.Background(), session.TeamID)
	}

	res := &domain.RevealResult{
		Target: session.TargetCandidate,
		Stats: domain.SessionStats{
			TimeSeconds:    session.GetElapsedTime(),
			QuestionsAsked: session.GetQuestionsAskedCount(),
			FailedRequests: session.FailedRequests,
		},
		SessionEnded: session.Completed,
	}
	return res, nil
}

func (s *sessionService) updateTeamStats(session *domain.Session, score int, correct bool, solvedCandidateID string) {
	ctx := context.Background()

	if correct && solvedCandidateID != "" {
		// Atomically update score, solves, solved-characters set, leaderboard, and masterboard
		// in a single Lua script to prevent race conditions.
		if err := s.dbStore.UpdateTeamStatsAtomic(ctx, session.TeamID, solvedCandidateID, score); err != nil {
			log.Printf("Error in atomic team stats update for team %s: %v", session.TeamID, err)
			return
		}

		// Conditionally update fastest solve time (atomic compare-and-set via Lua).
		elapsed := time.Duration(session.GetElapsedTime()) * time.Second
		if err := s.dbStore.UpdateFastestSolve(ctx, session.TeamID, elapsed); err != nil {
			log.Printf("Error updating fastest solve for team %s: %v", session.TeamID, err)
		}
	} else {
		// Wrong-guess penalty: only update score and leaderboard position.
		if err := s.dbStore.IncrementTeamScore(ctx, session.TeamID, score); err != nil {
			log.Printf("Error incrementing team score for team %s: %v", session.TeamID, err)
		}
	}

	// Clear the active session ID so the team can start a new session.
	if err := s.dbStore.ClearActiveSession(ctx, session.TeamID); err != nil {
		log.Printf("Error clearing active session for team %s: %v", session.TeamID, err)
	}
}
