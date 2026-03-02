package service

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/guesswho/internal/db"
	"github.com/guesswho/internal/domain"
	"github.com/guesswho/internal/logging"
)

// ErrTooManySessions is returned by StartSession when a team already has the
// maximum number of concurrent active sessions (2).
var ErrTooManySessions = errors.New("too many active sessions for this team")

// SessionService manages game sessions
type SessionService interface {
	StartSession(ctx context.Context, teamID string) (*domain.Session, error)
	GetSession(ctx context.Context, sessionID string) (*domain.Session, error)
	AskQuestion(ctx context.Context, sessionID string, questionID string, value string) (*domain.TraitAnswer, error)
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

// generateRandomSeed returns a cryptographically random int64 seed.
// Falls back to time.Now().UnixNano() if the OS entropy source is unavailable,
// which is still not predictable from the session ID.
func generateRandomSeed() int64 {
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		return time.Now().UnixNano()
	}
	return int64(binary.LittleEndian.Uint64(b[:]))
}

func (s *sessionService) StartSession(ctx context.Context, teamID string) (*domain.Session, error) {
	// HP3: Enforce per-team concurrent session limit (max 2 active sessions).
	count, err := s.dbStore.CountActiveSessions(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("failed to check active sessions: %w", err)
	}
	if count >= 2 {
		return nil, ErrTooManySessions
	}

	sessionID := fmt.Sprintf("s_%s", uuid.New().String()[:8])

	// Generate a cryptographically random seed so that the board order and
	// target character cannot be predicted from the session ID.
	perSessionSeed := generateRandomSeed()

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

	session.Candidates = availableCandidates

	// Generate per-session fake ID mapping for anti-cheat.
	candidateIDMap := make(map[string]string, len(availableCandidates))
	candidateIDMapRev := make(map[string]string, len(availableCandidates))
	for i, c := range availableCandidates {
		fakeID := fmt.Sprintf("P%02d", i+1)
		candidateIDMap[fakeID] = c.CandidateID
		candidateIDMapRev[c.CandidateID] = fakeID
	}
	session.CandidateIDMap = candidateIDMap
	session.CandidateIDMapRev = candidateIDMapRev

	// For each candidate, independently shuffle all trait keys and assign a
	// unique random 20-trait visible subset.
	allTraitDefs := s.traitCatalog.GetAllTraits()
	baseTraitKeys := make([]string, len(allTraitDefs))
	for i, td := range allTraitDefs {
		baseTraitKeys[i] = td.TraitKey
	}
	subsetSize := 20
	if len(baseTraitKeys) < subsetSize {
		subsetSize = len(baseTraitKeys)
	}
	rng := rand.New(rand.NewSource(perSessionSeed))
	candidateTraitSubsets := make(map[string][]string, len(availableCandidates))
	for _, c := range availableCandidates {
		shuffled := make([]string, len(baseTraitKeys))
		copy(shuffled, baseTraitKeys)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		candidateTraitSubsets[c.CandidateID] = shuffled[:subsetSize]
	}
	session.CandidateTraitSubsets = candidateTraitSubsets

	// Select a random target using the per-session seed.
	session.TargetCandidate = s.boardGenerator.SelectTarget(availableCandidates, perSessionSeed)

	if session.TargetCandidate == nil {
		return nil, fmt.Errorf("failed to select target candidate")
	}

	// Save session
	if err := s.dbStore.WriteSession(ctx, session); err != nil {
		return nil, fmt.Errorf("failed to save session: %w", err)
	}

	// HP3: Register this session in the team's active-sessions set.
	if err := s.dbStore.AddActiveSession(ctx, teamID, sessionID); err != nil {
		logging.Warn(ctx, "failed to add session to active sessions set", "error", err)
	}

	// If all characters were solved, clear the solved set in Redis.
	if resetOccurred {
		if err := s.dbStore.ClearSolvedCharacters(ctx, teamID); err != nil {
			logging.Warn(ctx, "failed to clear solved characters", "error", err)
		}
	}

	// Set the active session ID using a targeted HSET.
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

// AskQuestion processes a trait question for the given session.
//
// For boolean traits, value is ignored and the answer is the trait's boolean value.
// For enum/numeric traits, value must be provided; the answer is true if the
// target's trait value matches value (case-insensitive), false otherwise.
//
// Stage 1 flow:
//  1. Roll for block (25%) — if blocked, return a blocked response without recording the question.
//  2. Record the question and reveal the trait on the board.
//  3. Roll for encryption (40%) — if encrypted, return ciphertext + cipherType.
//  4. Otherwise return the plain boolean answer.
func (s *sessionService) AskQuestion(ctx context.Context, sessionID string, questionID string, value string) (*domain.TraitAnswer, error) {
	logging.Debug(ctx, "processing question", "questionId", questionID)

	lockValue, err := s.dbStore.AcquireSessionLock(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer s.dbStore.ReleaseSessionLock(ctx, sessionID, lockValue)

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

	// Enum traits require the player to specify the value they are asking about.
	if traitDef.Type == domain.TraitTypeEnum && value == "" {
		return nil, fmt.Errorf("enum trait requires a value: specify the value you are asking about for trait '%s'", traitDef.TraitKey)
	}

	// Check for chaos/failure injection — returns a ChaosError (real HTTP error)
	if s.chaosService.ShouldFail(session) {
		session.IncrementFailure("failed")
		s.dbStore.WriteSession(ctx, session)
		logging.Info(ctx, "chaos: injecting failure", "questionId", questionID)
		return nil, s.chaosService.GetChaosError()
	}

	// Get the answer from the target candidate
	answer, exists := session.TargetCandidate.GetTrait(traitDef.TraitKey)
	if !exists {
		return nil, fmt.Errorf("trait not found on target: %s", traitDef.TraitKey)
	}

	// Convert the raw trait value to a boolean answer.
	var boolAnswer bool
	switch traitDef.Type {
	case domain.TraitTypeBoolean:
		boolAnswer, _ = answer.(bool)
	default:
		// enum or numeric: player asked "is it <value>?"
		actualValue := fmt.Sprintf("%v", answer)
		boolAnswer = strings.EqualFold(actualValue, value)
	}

	// Stage 1 — Step 1: Roll for block (25% chance).
	// If blocked, do NOT record the question and do NOT count it.
	rollBytes := make([]byte, 1)
	crand.Read(rollBytes)
	if int(rollBytes[0])%100 < 25 {
		logging.Debug(ctx, "trait blocked by Stage 1 filter", "questionId", questionID)
		return &domain.TraitAnswer{
			QuestionID: questionID,
			TraitKey:   traitDef.TraitKey,
			Blocked:    true,
			Message:    "This trait is classified. You are not permitted to know this value.",
		}, nil
	}

	// Record the question and reveal the queried trait on the board (session-wide).
	session.RecordQuestion(questionID)
	session.RevealTrait(traitDef.TraitKey)

	// Stage 1 — Step 2: Roll for encryption (40% chance).
	// The decision is stored per-questionID so repeated decode calls are consistent.
	shouldEncrypt := false
	if decided, alreadyDecided := session.EncryptedQuestions[questionID]; alreadyDecided {
		shouldEncrypt = decided
	} else {
		crand.Read(rollBytes)
		shouldEncrypt = int(rollBytes[0])%100 < 40 // 40% chance
		if session.EncryptedQuestions == nil {
			session.EncryptedQuestions = make(map[string]bool)
		}
		session.EncryptedQuestions[questionID] = shouldEncrypt
	}

	// Persist all session mutations in a single write.
	s.dbStore.WriteSession(ctx, session)

	// Milestones are atomic via Lua script, safe to call without session lock concerns.
	// M2: First Successful Question — awarded after the first non-degraded answer.
	s.milestoneService.AwardIfAbsent(ctx, session.TeamID, domain.MilestoneM2)

	// M3: Elimination Working — awarded when >= 3 questions have been asked in a session.
	if session.GetQuestionsAskedCount() >= 3 {
		s.milestoneService.AwardIfAbsent(ctx, session.TeamID, domain.MilestoneM3)
	}

	// S3: Resilience — awarded when a successful answer is received during an active chaos window.
	if s.chaosService.IsInChaosWindow(session) {
		s.milestoneService.AwardIfAbsent(ctx, session.TeamID, domain.MilestoneS3)
	}

	// Build response
	traitAnswer := &domain.TraitAnswer{
		QuestionID: questionID,
		TraitKey:   traitDef.TraitKey,
	}

	if shouldEncrypt {
		plaintext := fmt.Sprintf("%v", boolAnswer)
		encrypted, err := s.encryptionService.Encrypt(plaintext, session.EncryptCipher, session.EncryptKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt answer: %w", err)
		}
		encTrue := true
		traitAnswer.Encrypted = &encTrue
		traitAnswer.Ciphertext = encrypted
		traitAnswer.CipherType = session.EncryptCipher
	} else {
		encFalse := false
		traitAnswer.Encrypted = &encFalse
		traitAnswer.Answer = &boolAnswer
	}

	logging.Debug(ctx, "question answered", "questionId", questionID, "encrypted", shouldEncrypt, "cipher", session.EncryptCipher)

	return traitAnswer, nil
}

func (s *sessionService) SubmitGuess(ctx context.Context, sessionID string, candidateID string) (*domain.GuessResult, error) {
	lockValue, err := s.dbStore.AcquireSessionLock(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer s.dbStore.ReleaseSessionLock(ctx, sessionID, lockValue)

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
		logging.Info(ctx, "chaos: injecting failure on guess", "candidateId", candidateID)
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
	session.RecordGuess(realCandidateID)
	if correct {
		session.CorrectGuess = true
		session.MarkComplete(true)
	} else {
		// Wrong guess: apply -200 penalty immediately.
		if err := s.dbStore.IncrementTeamScore(ctx, session.TeamID, -200); err != nil {
			logging.Error(ctx, "failed to apply wrong-guess penalty", "error", err)
		}
		session.DecrementGuess()
	}

	// End session if all candidates have been guessed or guesses are exhausted
	if !session.Completed && (session.GuessesRemaining <= 0 || session.GetGuessedCount() >= len(session.Candidates)) {
		session.MarkComplete(false)
	}

	// Persist updates
	_ = s.dbStore.WriteSession(ctx, session)

	// Record on leaderboard only when the session actually ends
	if session.Completed {
		finalCorrect := session.CorrectGuess
		finalScore := 0
		if finalCorrect {
			finalScore = totalScore
		}
		s.updateTeamStats(ctx, session, finalScore, finalCorrect, realCandidateID)
	}

	// Milestones are atomic via Lua script
	if correct {
		// M4: First Correct Solve — awarded on the first correct guess.
		s.milestoneService.AwardIfAbsent(ctx, session.TeamID, domain.MilestoneM4)

		// S1: Efficiency — awarded when a character is solved with <= 10 questions.
		if session.GetQuestionsAskedCount() <= 10 {
			s.milestoneService.AwardIfAbsent(ctx, session.TeamID, domain.MilestoneS1)
		}
	}

	// S2: Automation — awarded once the team reaches 3+ total correct solves.
	if session.Completed && session.CorrectGuess {
		if teamData, err := s.dbStore.ReadTeamData(ctx, session.TeamID); err == nil && teamData.Solves >= 3 {
			s.milestoneService.AwardIfAbsent(ctx, session.TeamID, domain.MilestoneS2)
		}
	}

	// Publish an update now that all data is persisted.
	if correct {
		if err := s.dbStore.PublishGameUpdate(ctx, session.TeamID, realCandidateID); err != nil {
			logging.Error(ctx, "failed to publish game update", "error", err)
		}
	} else {
		if err := s.dbStore.PublishGameUpdate(ctx, session.TeamID, ""); err != nil {
			logging.Error(ctx, "failed to publish game update", "error", err)
		}
	}

	logging.Info(ctx, "guess submitted", "fakeId", candidateID, "realId", realCandidateID, "correct", correct, "score", totalScore, "guessesRemaining", session.GuessesRemaining)

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
	lockValue, err := s.dbStore.AcquireSessionLock(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer s.dbStore.ReleaseSessionLock(ctx, sessionID, lockValue)

	session, err := s.dbStore.ReadSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// Chaos injection
	if s.chaosService.ShouldFail(session) {
		session.IncrementFailure("failed")
		s.dbStore.WriteSession(ctx, session)
		logging.Info(ctx, "chaos: injecting failure on reveal")
		return nil, s.chaosService.GetChaosError()
	}

	alreadyCompleted := session.Completed

	// Restriction A: require at least 5 questions before reveal is available.
	if !alreadyCompleted && session.GetQuestionsAskedCount() < 5 {
		return nil, fmt.Errorf("must ask at least 5 questions before revealing")
	}

	// If not already completed, mark the session as completed.
	if !alreadyCompleted {
		// Restriction B: apply a -500 point penalty for using reveal.
		if err := s.dbStore.IncrementTeamScore(ctx, session.TeamID, -500); err != nil {
			logging.Error(ctx, "failed to apply reveal penalty", "error", err)
		}
		session.MarkComplete(session.CorrectGuess)
		_ = s.dbStore.WriteSession(ctx, session)

		_ = s.dbStore.ClearActiveSession(ctx, session.TeamID)

		// HP3: Remove this session from the team's active-sessions set.
		if err := s.dbStore.RemoveActiveSession(ctx, session.TeamID, session.SessionID); err != nil {
			logging.Error(ctx, "failed to remove session from active sessions set on reveal", "error", err)
		}
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

	// Clear the active session ID so the team can start a new session.
	if err := s.dbStore.ClearActiveSession(ctx, session.TeamID); err != nil {
		logging.Error(ctx, "failed to clear active session", "error", err)
	}

	// HP3: Remove this session from the team's active-sessions set.
	if err := s.dbStore.RemoveActiveSession(ctx, session.TeamID, session.SessionID); err != nil {
		logging.Error(ctx, "failed to remove session from active sessions set", "error", err)
	}
}
