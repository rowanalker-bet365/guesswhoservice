package repository

import (
	"fmt"
	"sync"

	"github.com/guesswho/internal/domain"
)

// SessionRepository manages session storage
type SessionRepository interface {
	Save(session *domain.Session) error
	GetByID(sessionID string) (*domain.Session, error)
	GetByTeamID(teamID string) []*domain.Session
	Delete(sessionID string) error
}

type inMemorySessionRepository struct {
	mu       sync.RWMutex
	sessions map[string]*domain.Session
}

// NewInMemorySessionRepository creates a new in-memory session repository
func NewInMemorySessionRepository() SessionRepository {
	return &inMemorySessionRepository{
		sessions: make(map[string]*domain.Session),
	}
}

func (r *inMemorySessionRepository) Save(session *domain.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if session == nil {
		return fmt.Errorf("session cannot be nil")
	}

	r.sessions[session.SessionID] = session
	return nil
}

func (r *inMemorySessionRepository) GetByID(sessionID string) (*domain.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	session, exists := r.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	return session, nil
}

func (r *inMemorySessionRepository) GetByTeamID(teamID string) []*domain.Session {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*domain.Session
	for _, session := range r.sessions {
		if session.TeamID == teamID {
			result = append(result, session)
		}
	}

	return result
}

func (r *inMemorySessionRepository) Delete(sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.sessions[sessionID]; !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	delete(r.sessions, sessionID)
	return nil
}
