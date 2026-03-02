package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/guesswho/internal/logging"
)

// RateLimiter implements a simple token bucket rate limiter
type RateLimiter struct {
	enabled bool
	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens     int
	lastRefill time.Time
	capacity   int
	refillRate int // tokens per second
}

// NewRateLimiter creates a new rate limiter middleware
func NewRateLimiter(enabled bool) *RateLimiter {
	rl := &RateLimiter{
		enabled: enabled,
		buckets: make(map[string]*bucket),
	}
	go rl.cleanupLoop()
	return rl
}

// Limit returns a middleware that enforces rate limiting
func (rl *RateLimiter) Limit(capacity, refillRate int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Use the authenticated team ID from the request context (set by the JWT
			// auth middleware) when available. For unauthenticated requests that reach
			// the rate limiter, fall back to the client IP so they are still limited.
			teamID, ok := r.Context().Value(TeamIDKey).(string)
			var key string
			if ok && teamID != "" {
				key = "team:" + teamID + ":" + r.URL.Path
			} else {
				ip, _, err := net.SplitHostPort(r.RemoteAddr)
				if err != nil {
					ip = r.RemoteAddr
				}
				key = "ip:" + ip + ":" + r.URL.Path
			}

			if !rl.allow(key, capacity, refillRate) {
				logging.Warn(r.Context(), "rate limit exceeded", "teamId", teamID, "path", r.URL.Path)
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (rl *RateLimiter) allow(key string, capacity, refillRate int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, exists := rl.buckets[key]
	if !exists {
		b = &bucket{
			tokens:     capacity,
			lastRefill: time.Now(),
			capacity:   capacity,
			refillRate: refillRate,
		}
		rl.buckets[key] = b
	}

	// Refill tokens based on time elapsed
	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	tokensToAdd := int(elapsed * float64(b.refillRate))

	if tokensToAdd > 0 {
		b.tokens += tokensToAdd
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
		b.lastRefill = now
	}

	// Check if we have tokens
	if b.tokens > 0 {
		b.tokens--
		return true
	}

	return false
}

// cleanupLoop periodically evicts stale rate limiter buckets to prevent unbounded memory growth.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for key, b := range rl.buckets {
			if now.Sub(b.lastRefill) > 10*time.Minute {
				delete(rl.buckets, key)
			}
		}
		rl.mu.Unlock()
	}
}
