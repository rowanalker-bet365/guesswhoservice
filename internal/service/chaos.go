package service

import (
	"math/rand"
	"time"

	"github.com/guesswho/internal/domain"
)

// ChaosService manages failure injection for sessions
type ChaosService interface {
	ShouldFail(session *domain.Session, traitDef *domain.TraitDefinition) bool
	GetRetryDelay() int
	// IsInChaosWindow returns true when the session is currently inside a chaos
	// window — i.e. the elapsed time falls within the failure-injection period.
	// Used by MilestoneService to determine whether S3 (Resilience) can be awarded.
	IsInChaosWindow(session *domain.Session) bool
}

type chaosService struct {
	enabled bool
	rng     *rand.Rand
}

// NewChaosService creates a new chaos service
func NewChaosService(enabled bool) ChaosService {
	return &chaosService{
		enabled: enabled,
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (s *chaosService) ShouldFail(session *domain.Session, traitDef *domain.TraitDefinition) bool {
	if !s.enabled {
		return false
	}

	// Only flaky traits can fail
	if !traitDef.IsFlaky {
		return false
	}

	// Check if we're in a chaos window
	if !s.isInChaosWindow(session) {
		return false
	}

	// Probabilistic failure during chaos window
	// Higher failure rate for flaky traits
	failureRate := 0.6 // 60% failure rate during chaos window
	return s.rng.Float64() < failureRate
}

func (s *chaosService) GetRetryDelay() int {
	// Random retry delay between 500ms and 2000ms
	return 500 + s.rng.Intn(1500)
}

// IsInChaosWindow is the public implementation of the ChaosService interface method.
// It delegates to the unexported isInChaosWindow helper so the internal logic
// remains shared between ShouldFail and this public method.
func (s *chaosService) IsInChaosWindow(session *domain.Session) bool {
	return s.isInChaosWindow(session)
}

func (s *chaosService) isInChaosWindow(session *domain.Session) bool {
	if session.ChaosProfile.Mode != domain.ChaosModeScheduled {
		return false
	}

	elapsed := time.Since(session.CreatedAt).Seconds()
	intervalSeconds := float64(session.ChaosProfile.IntervalSeconds)
	windowSeconds := float64(session.ChaosProfile.WindowSeconds)

	if intervalSeconds == 0 {
		intervalSeconds = 240 // Default 4 minutes
	}
	if windowSeconds == 0 {
		windowSeconds = 90 // Default 90 seconds
	}

	// Calculate position within the current interval
	positionInInterval := float64(int(elapsed) % int(intervalSeconds))

	// Check if we're in the chaos window (starts at the beginning of each interval)
	return positionInInterval < windowSeconds
}
