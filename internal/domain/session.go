package domain

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"math/rand"
	"time"
)

// ChaosMode represents the type of failure injection
type ChaosMode string

const (
	ChaosModeScheduled     ChaosMode = "scheduled"
	ChaosModeProbabilistic ChaosMode = "probabilistic"
)

// ChaosProfile defines the failure injection behavior for a session
type ChaosProfile struct {
	Mode            ChaosMode `json:"mode"`
	WindowSeconds   int       `json:"windowSeconds"`
	IntervalSeconds int       `json:"intervalSeconds,omitempty"`
}

// Session represents a game session
type Session struct {
	SessionID             string              `json:"sessionId"`
	TeamID                string              `json:"teamId"`
	BoardSize             int                 `json:"boardSize"`
	TraitsAvailable       int                 `json:"traitsAvailable"`
	GuessLimit            int                 `json:"guessLimit"`
	ChaosProfile          ChaosProfile        `json:"chaosProfile"`
	Candidates            []*Candidate        `json:"candidates"`
	TargetCandidate       *Candidate          `json:"targetCandidate"`
	Seed                  int64               `json:"-"` // never expose to clients
	CreatedAt             time.Time           `json:"createdAt"`
	QuestionsAsked        []string            `json:"questionsAsked"`
	FailedRequests        int                 `json:"failedRequests"`
	Timeouts              int                 `json:"timeouts"`
	Unhandled5xx          int                 `json:"unhandled5xx"`
	GuessesRemaining      int                 `json:"guessesRemaining"`
	Completed             bool                `json:"completed"`
	CorrectGuess          bool                `json:"correctGuess"`
	GuessedCandidates     map[string]bool     `json:"guessedCandidates"`
	CandidateIDMap        map[string]string   `json:"candidateIdMap"`    // fakeID -> realID
	CandidateIDMapRev     map[string]string   `json:"candidateIdMapRev"` // realID -> fakeID
	EncryptKey                string              `json:"encryptKey"`
	EncryptCipher             string              `json:"encryptCipher"`
	EncryptedQuestions        map[string]bool     `json:"encryptedQuestions"`
	CandidateTraitSubsets     map[string][]string `json:"candidateTraitSubsets"` // per-candidate initial visible trait keys
	RevealedTraits            []string            `json:"revealedTraits"`        // trait names unlocked via questions (session-wide)
	Stage                     int                 `json:"stage"`
	PendingDecryption         bool                `json:"pendingDecryption"`
	PendingGuessCorrect       bool                `json:"pendingGuessCorrect"`
	PendingDecryptionPlaintext string             `json:"pendingDecryptionPlaintext"`
	PendingScore              int                 `json:"pendingScore"`
	PendingEncryptedResponse  string              `json:"pendingEncryptedResponse"`
	UnencryptedTraitAnswers   map[string]string   `json:"unencryptedTraitAnswers"`
}

// NewSession creates a new game session
func NewSession(sessionID, teamID string, guessLimit int, seed int64, chaos ChaosProfile, stage int) *Session {
	// Generate a random 32-byte key for this session
	keyBytes := make([]byte, 32)
	if _, err := cryptorand.Read(keyBytes); err != nil {
		panic("failed to generate encryption key: " + err.Error())
	}
	encryptKey := hex.EncodeToString(keyBytes) // 64-char hex string

	var cipherPool []string
	if stage == 2 {
		cipherPool = []string{"xor", "aes-128-gcm", "aes-256-cbc"}
	} else {
		cipherPool = []string{"caesar", "vigenere", "xor", "aes-128-gcm", "aes-256-cbc"}
	}
	r := rand.New(rand.NewSource(seed))
	encryptCipher := cipherPool[r.Intn(len(cipherPool))]

	return &Session{
		SessionID:               sessionID,
		TeamID:                  teamID,
		BoardSize:               64,
		TraitsAvailable:         64,
		GuessLimit:              guessLimit,
		ChaosProfile:            chaos,
		Seed:                    seed,
		CreatedAt:               time.Now(),
		QuestionsAsked:          make([]string, 0),
		GuessesRemaining:        guessLimit,
		Completed:               false,
		CorrectGuess:            false,
		GuessedCandidates:       make(map[string]bool),
		EncryptKey:              encryptKey,
		EncryptCipher:           encryptCipher,
		EncryptedQuestions:      make(map[string]bool),
		CandidateTraitSubsets:   make(map[string][]string),
		RevealedTraits:          make([]string, 0),
		Stage:                   stage,
		UnencryptedTraitAnswers: make(map[string]string),
	}
}

// RecordQuestion records that a question was asked
func (s *Session) RecordQuestion(questionID, traitKey, answer string, encrypted bool) {
	s.QuestionsAsked = append(s.QuestionsAsked, questionID)
	s.EncryptedQuestions[questionID] = encrypted
	s.RevealTrait(traitKey)
	if !encrypted {
		if s.UnencryptedTraitAnswers == nil {
			s.UnencryptedTraitAnswers = make(map[string]string)
		}
		s.UnencryptedTraitAnswers[traitKey] = answer
	}
}

// IncrementFailure increments the failure counters
func (s *Session) IncrementFailure(failureType string) {
	switch failureType {
	case "failed":
		s.FailedRequests++
	case "timeout":
		s.Timeouts++
	case "5xx":
		s.Unhandled5xx++
	}
}

// MarkComplete marks the session as completed
func (s *Session) MarkComplete(correct bool) {
	s.Completed = true
	s.CorrectGuess = correct
}

// DecrementGuess decrements remaining failed guesses
func (s *Session) DecrementGuess() {
	if s.GuessesRemaining > 0 {
		s.GuessesRemaining--
	}
}

// RecordGuess records that a candidate has been guessed
func (s *Session) RecordGuess(candidateID string) {
	if s.GuessedCandidates == nil {
		s.GuessedCandidates = make(map[string]bool)
	}
	s.GuessedCandidates[candidateID] = true
}

// HasGuessedCandidate returns true if the candidate has been guessed at least once
func (s *Session) HasGuessedCandidate(candidateID string) bool {
	if s.GuessedCandidates == nil {
		return false
	}
	return s.GuessedCandidates[candidateID]
}

// GetGuessedCount returns the number of unique candidates guessed
func (s *Session) GetGuessedCount() int {
	return len(s.GuessedCandidates)
}

// GetElapsedTime returns the time since session creation
func (s *Session) GetElapsedTime() int {
	return int(time.Since(s.CreatedAt).Seconds())
}

// GetQuestionsAskedCount returns the number of questions asked
func (s *Session) GetQuestionsAskedCount() int {
	return len(s.QuestionsAsked)
}

// IsTraitVisibleForCandidate returns true if the given trait name is in that
// candidate's initial subset or has been revealed session-wide via a question.
func (s *Session) IsTraitVisibleForCandidate(candidateID, traitName string) bool {
	// Legacy sessions (created before HP3) have no subset map — show all traits.
	// L6: use len() == 0 to cover both nil maps and partially-initialised empty maps.
	if len(s.CandidateTraitSubsets) == 0 {
		return true
	}
	for _, t := range s.CandidateTraitSubsets[candidateID] {
		if t == traitName {
			return true
		}
	}
	for _, t := range s.RevealedTraits {
		if t == traitName {
			return true
		}
	}
	return false
}

// RevealTrait adds a trait name to RevealedTraits if not already present.
func (s *Session) RevealTrait(traitName string) {
	for _, t := range s.RevealedTraits {
		if t == traitName {
			return
		}
	}
	s.RevealedTraits = append(s.RevealedTraits, traitName)
}
