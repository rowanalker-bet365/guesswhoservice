package domain

import (
	cryptorand "crypto/rand"
	"encoding/hex"
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
	SessionID         string            `json:"sessionId"`
	TeamID            string            `json:"teamId"`
	BoardSize         int               `json:"boardSize"`
	TraitsAvailable   int               `json:"traitsAvailable"`
	GuessLimit        int               `json:"guessLimit"`
	ChaosProfile      ChaosProfile      `json:"chaosProfile"`
	Candidates        []*Candidate      `json:"candidates"`
	TargetCandidate   *Candidate        `json:"targetCandidate"`
	Seed              int64             `json:"seed"`
	CreatedAt         time.Time         `json:"createdAt"`
	QuestionsAsked    []string          `json:"questionsAsked"`
	FailedRequests    int               `json:"failedRequests"`
	Timeouts          int               `json:"timeouts"`
	Unhandled5xx      int               `json:"unhandled5xx"`
	GuessesRemaining  int               `json:"guessesRemaining"`
	Completed         bool              `json:"completed"`
	CorrectGuess      bool              `json:"correctGuess"`
	GuessedCandidates map[string]bool   `json:"guessedCandidates"`
	CandidateIDMap    map[string]string `json:"candidateIdMap"`    // fakeID -> realID
	CandidateIDMapRev map[string]string `json:"candidateIdMapRev"` // realID -> fakeID
	EncryptKey        string            `json:"encryptKey"`
	EncryptCipher      string            `json:"encryptCipher"`
	EncryptedQuestions map[string]bool   `json:"encryptedQuestions"`
}

// NewSession creates a new game session
func NewSession(sessionID, teamID string, guessLimit int, seed int64, chaos ChaosProfile) *Session {
	// Generate a random 8-byte key for this session (used by caesar and xor ciphers)
	keyBytes := make([]byte, 8)
	if _, err := cryptorand.Read(keyBytes); err != nil {
		// Fallback: use a deterministic key from the session ID
		keyBytes = []byte(sessionID + "0000000000000000")[:8]
	}
	encryptKey := hex.EncodeToString(keyBytes)

	// Randomly select a cipher for this session using crypto/rand
	ciphers := []string{"base64", "hex", "reverse", "caesar", "xor", "vigenere", "xor-base64"}
	idxBytes := make([]byte, 1)
	cryptorand.Read(idxBytes)
	encryptCipher := ciphers[int(idxBytes[0])%len(ciphers)]

	return &Session{
		SessionID:         sessionID,
		TeamID:            teamID,
		BoardSize:         64,
		TraitsAvailable:   64,
		GuessLimit:        guessLimit,
		ChaosProfile:      chaos,
		Seed:              seed,
		CreatedAt:         time.Now(),
		QuestionsAsked:    make([]string, 0),
		GuessesRemaining:  guessLimit,
		Completed:         false,
		CorrectGuess:      false,
		GuessedCandidates: make(map[string]bool),
		EncryptKey:         encryptKey,
		EncryptCipher:      encryptCipher,
		EncryptedQuestions: make(map[string]bool),
	}
}

// RecordQuestion records that a question was asked
func (s *Session) RecordQuestion(questionID string) {
	s.QuestionsAsked = append(s.QuestionsAsked, questionID)
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
