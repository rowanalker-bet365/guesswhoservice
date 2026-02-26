package service

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/guesswho/internal/db"
	"github.com/guesswho/internal/domain"
)

// SessionService manages game sessions
type SessionService interface {
	StartSession(teamID string) (*domain.Session, error)
	GetSession(sessionID string) (*domain.Session, error)
	AskQuestion(sessionID string, questionID string) (*domain.TraitAnswer, error)
	SubmitGuess(sessionID string, candidateID string) (*domain.GuessResult, error)
	Reveal(sessionID string) (*domain.RevealResult, error)
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
		chaosEnabled:      config.ChaosEnabled,
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

func (s *sessionService) StartSession(teamID string) (*domain.Session, error) {
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

	// Track whether a full reset occurred before overwriting availableCandidates.
	resetOccurred := len(availableCandidates) == 0

	// If all candidates are solved, reset the solved list and use all candidates
	if resetOccurred {
		availableCandidates = candidates
	}

	// GenerateBoard already shuffled the candidates with perSessionSeed, so
	// session.Candidates is already in a unique per-session order.
	session.Candidates = availableCandidates

	// Select a random target using the per-session seed (SelectTarget uses
	// seed+1000 internally so it does not collide with the board shuffle).
	session.TargetCandidate = s.boardGenerator.SelectTarget(availableCandidates, perSessionSeed)

	if session.TargetCandidate == nil {
		return nil, fmt.Errorf("failed to select target candidate")
	}

	// Save session
	if err := s.dbStore.WriteSession(context.Background(), session); err != nil {
		return nil, fmt.Errorf("failed to save session: %w", err)
	}

	// If all characters were solved, clear the solved set in Redis so the team starts fresh.
	if resetOccurred {
		if err := s.dbStore.ClearSolvedCharacters(context.Background(), teamID); err != nil {
			log.Printf("Warning: failed to clear solved characters for team %s: %v", teamID, err)
		}
	}

	// Set the active session ID using a targeted HSET — do NOT use WriteTeamData here,
	// as that would overwrite score/solves/solved-characters with the stale snapshot.
	if err := s.dbStore.SetActiveSession(context.Background(), teamID, sessionID); err != nil {
		return nil, fmt.Errorf("failed to update active session for team %s: %w", teamID, err)
	}

	// M1: First Round Started — awarded once when a team starts their first session.
	s.milestoneService.AwardIfAbsent(context.Background(), teamID, domain.MilestoneM1)

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

	// M2: First Successful Question — awarded after the first non-degraded answer.
	s.milestoneService.AwardIfAbsent(context.Background(), session.TeamID, domain.MilestoneM2)

	// M3: Elimination Working — awarded when ≥ 3 questions have been asked in a session.
	if session.GetQuestionsAskedCount() >= 3 {
		s.milestoneService.AwardIfAbsent(context.Background(), session.TeamID, domain.MilestoneM3)
	}

	// S3: Resilience — awarded when a successful answer is received for a flaky trait
	// while the session is inside a chaos window. Chaos must be enabled for this to trigger.
	if traitDef.IsFlaky && s.chaosService.IsInChaosWindow(session) {
		s.milestoneService.AwardIfAbsent(context.Background(), session.TeamID, domain.MilestoneS3)
	}

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
			ReliabilityPenalty: 200,
		}
		totalScore = -200
	}

	// Update session state
	//  - Track unique candidates guessed
	//  - Only decrement failed guesses on wrong guesses
	session.RecordGuess(candidateID)
	if correct {
		session.CorrectGuess = true
		// Mark session as complete immediately on correct guess
		session.MarkComplete(true)

		// M4: First Correct Solve — awarded on the first correct guess.
		s.milestoneService.AwardIfAbsent(context.Background(), session.TeamID, domain.MilestoneM4)

		// S1: Efficiency — awarded when a character is solved with ≤ 10 questions.
		if session.GetQuestionsAskedCount() <= 10 {
			s.milestoneService.AwardIfAbsent(context.Background(), session.TeamID, domain.MilestoneS1)
		}
	} else {
		// Wrong guess: apply -200 penalty immediately, regardless of whether the session ends.
		if err := s.dbStore.IncrementTeamScore(context.Background(), session.TeamID, -200); err != nil {
			log.Printf("Error applying wrong-guess penalty for team %s: %v", session.TeamID, err)
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
			finalScore = 0
		}

		// Update team stats including solved characters and clearing active session
		s.updateTeamStats(session, finalScore, finalCorrect, candidateID)

		// S2: Automation — awarded once the team reaches 3+ total correct solves.
		// Read the updated solve count after updateTeamStats has incremented it.
		if finalCorrect {
			if teamData, err := s.dbStore.ReadTeamData(context.Background(), session.TeamID); err == nil && teamData.Solves >= 3 {
				s.milestoneService.AwardIfAbsent(context.Background(), session.TeamID, domain.MilestoneS2)
			}
		}
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
	}
	// Wrong-guess penalties are applied immediately in SubmitGuess, not here.

	// Clear the active session ID so the team can start a new session.
	if err := s.dbStore.ClearActiveSession(ctx, session.TeamID); err != nil {
		log.Printf("Error clearing active session for team %s: %v", session.TeamID, err)
	}
}
