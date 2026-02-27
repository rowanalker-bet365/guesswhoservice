package service

import (
	"math/rand"
	"sync"
	"time"

	"github.com/guesswho/internal/domain"
)

// ChaosService manages failure injection for sessions
type ChaosService interface {
	// ShouldFail returns true if the current request should fail due to chaos.
	// No longer trait-dependent — any request during a chaos window can fail.
	ShouldFail(session *domain.Session) bool

	// GetChaosError returns a randomized HTTP error for a chaos failure.
	GetChaosError() *ChaosError

	// IsInChaosWindow returns true if the session is currently in a chaos window.
	IsInChaosWindow(session *domain.Session) bool

	// Enable turns chaos mode on dynamically.
	Enable()

	// Disable turns chaos mode off dynamically.
	Disable()

	// IsEnabled returns the current chaos state.
	IsEnabled() bool

	// GetWindowConfig returns the chaos window configuration.
	GetWindowConfig() (windowSeconds, intervalSeconds int)
}

type chaosService struct {
	mu              sync.RWMutex
	enabled         bool
	rng             *rand.Rand
	windowSeconds   int
	intervalSeconds int
}

// NewChaosService creates a new chaos service with window configuration.
func NewChaosService(enabled bool, windowSeconds, intervalSeconds int) ChaosService {
	if windowSeconds <= 0 {
		windowSeconds = 90
	}
	if intervalSeconds <= 0 {
		intervalSeconds = 240
	}
	return &chaosService{
		enabled:         enabled,
		rng:             rand.New(rand.NewSource(time.Now().UnixNano())),
		windowSeconds:   windowSeconds,
		intervalSeconds: intervalSeconds,
	}
}

func (s *chaosService) Enable() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = true
}

func (s *chaosService) Disable() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = false
}

func (s *chaosService) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

func (s *chaosService) GetWindowConfig() (windowSeconds, intervalSeconds int) {
	return s.windowSeconds, s.intervalSeconds
}

func (s *chaosService) ShouldFail(session *domain.Session) bool {
	if !s.IsEnabled() {
		return false
	}

	// Check if we're in a chaos window
	if !s.isInChaosWindow(session) {
		return false
	}

	// 60% failure rate during chaos window
	s.mu.Lock()
	roll := s.rng.Float64()
	s.mu.Unlock()

	return roll < 0.6
}

// GetChaosError returns a randomized HTTP error simulating real server failures.
// Weighted distribution:
//   - 503 Service Unavailable: 35%
//   - 429 Too Many Requests: 25%
//   - 500 Internal Server Error: 25%
//   - 408 Request Timeout: 15%
func (s *chaosService) GetChaosError() *ChaosError {
	s.mu.Lock()
	roll := s.rng.Float64()
	retryMs := 500 + s.rng.Intn(2500) // 500-3000ms
	s.mu.Unlock()

	switch {
	case roll < 0.35:
		return &ChaosError{
			StatusCode:   503,
			ErrorCode:    "service_unavailable",
			Message:      "Service temporarily unavailable. The server is experiencing intermittent issues. Retry after the suggested delay.",
			RetryAfterMs: retryMs,
		}
	case roll < 0.60:
		return &ChaosError{
			StatusCode:   429,
			ErrorCode:    "rate_limited",
			Message:      "Too many requests. The server is under heavy load. Back off and retry with exponential delay.",
			RetryAfterMs: retryMs,
		}
	case roll < 0.85:
		return &ChaosError{
			StatusCode:   500,
			ErrorCode:    "internal_server_error",
			Message:      "An unexpected internal error occurred. The server encountered a transient fault. Please retry.",
			RetryAfterMs: retryMs,
		}
	default:
		return &ChaosError{
			StatusCode:   408,
			ErrorCode:    "request_timeout",
			Message:      "The request timed out. The server did not respond in time. Consider increasing your timeout and retrying.",
			RetryAfterMs: retryMs,
		}
	}
}

func (s *chaosService) IsInChaosWindow(session *domain.Session) bool {
	return s.isInChaosWindow(session)
}

func (s *chaosService) isInChaosWindow(session *domain.Session) bool {
	if !s.IsEnabled() {
		return false
	}

	if session.ChaosProfile.Mode != domain.ChaosModeScheduled {
		return false
	}

	elapsed := time.Since(session.CreatedAt).Seconds()
	intervalSec := float64(session.ChaosProfile.IntervalSeconds)
	windowSec := float64(session.ChaosProfile.WindowSeconds)

	if intervalSec == 0 {
		intervalSec = float64(s.intervalSeconds)
	}
	if windowSec == 0 {
		windowSec = float64(s.windowSeconds)
	}

	positionInInterval := float64(int(elapsed) % int(intervalSec))
	return positionInInterval < windowSec
}
