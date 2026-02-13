package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/guesswho/internal/handler"
	custommiddleware "github.com/guesswho/internal/middleware"
	"github.com/guesswho/internal/repository"
	"github.com/guesswho/internal/service"
	"github.com/guesswho/pkg/config"
)

type Middleware func(http.Handler) http.Handler

func chain(h http.Handler, m ...Middleware) http.Handler {
	for i := len(m) - 1; i >= 0; i-- {
		h = m[i](h)
	}
	return h
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		dur := time.Since(start)
		log.Printf("%s %s %s", r.Method, r.URL.Path, dur)
	})
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic recovered: %v", rec)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Team-Id, X-Api-Key")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func timeoutMiddleware(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, d, "request timed out")
	}
}

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize repositories
	sessionRepo := repository.NewInMemorySessionRepository()
	leaderboardRepo := repository.NewInMemoryLeaderboardRepository()

	// Initialize services
	traitCatalog := service.NewTraitCatalogService()
	boardGenerator := service.NewBoardGeneratorService()
	encryptionService := service.NewEncryptionService()
	chaosService := service.NewChaosService(cfg.ChaosEnabled)
	scoringService := service.NewScoringService()

	sessionService := service.NewSessionService(
		sessionRepo,
		leaderboardRepo,
		traitCatalog,
		boardGenerator,
		encryptionService,
		chaosService,
		scoringService,
		service.SessionServiceConfig{
			ChaosEnabled:  cfg.ChaosEnabled,
			ChaosInterval: cfg.ChaosIntervalSeconds,
			ChaosWindow:   cfg.ChaosWindowSeconds,
		},
	)

	// Initialize handlers
	sessionHandler := handler.NewSessionHandler(sessionService, traitCatalog)
	traitHandler := handler.NewTraitHandler(traitCatalog, encryptionService)
	leaderboardHandler := handler.NewLeaderboardHandler(leaderboardRepo)

	// Initialize middleware
	rateLimiter := custommiddleware.NewRateLimiter(cfg.RateLimitEnabled)

	// Setup mux with Go 1.22+ patterns
	mux := http.NewServeMux()

	// API routes
	mux.Handle("POST /v1/sessions/start", rateLimiter.Limit(10, 1)(http.HandlerFunc(sessionHandler.StartSession)))
	mux.HandleFunc("GET /v1/sessions/{sessionId}/board", sessionHandler.GetBoard)
	mux.HandleFunc("GET /v1/sessions/{sessionId}/questions", traitHandler.GetQuestions)
	mux.HandleFunc("GET /v1/sessions/{sessionId}/status", sessionHandler.Status)
	mux.Handle("POST /v1/sessions/{sessionId}/ask", rateLimiter.Limit(60, 5)(http.HandlerFunc(sessionHandler.AskQuestion)))
	mux.HandleFunc("POST /v1/sessions/{sessionId}/decode", traitHandler.Decode)
	mux.HandleFunc("POST /v1/sessions/{sessionId}/guess", sessionHandler.SubmitGuess)
	mux.HandleFunc("POST /v1/sessions/{sessionId}/reveal", sessionHandler.Reveal)

	// Leaderboard route
	mux.HandleFunc("GET /v1/leaderboard", leaderboardHandler.GetLeaderboard)

	// Health check endpoint
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	// Root endpoint with API info
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"service": "Guess Who API",
			"version": "1.0.0",
			"endpoints": {
				"start_session": "POST /v1/sessions/start",
				"get_board": "GET /v1/sessions/{sessionId}/board",
				"get_questions": "GET /v1/sessions/{sessionId}/questions",
				"session_status": "GET /v1/sessions/{sessionId}/status",
				"ask_question": "POST /v1/sessions/{sessionId}/ask",
				"decode": "POST /v1/sessions/{sessionId}/decode",
				"submit_guess": "POST /v1/sessions/{sessionId}/guess",
				"reveal": "POST /v1/sessions/{sessionId}/reveal",
				"leaderboard": "GET /v1/leaderboard",
				"health": "GET /health"
			}
		}`))
	})

	// Compose middleware stack
	handlerStack := chain(
		mux,
		corsMiddleware,
		recoverMiddleware,
		loggingMiddleware,
		timeoutMiddleware(60*time.Second),
	)

	// Start server
	addr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("🚀 Starting Guess Who API server on %s", addr)
	log.Printf("⚙️  Configuration:")
	log.Printf("   - Rate Limiting: %v", cfg.RateLimitEnabled)
	log.Printf("   - Chaos Mode: %v", cfg.ChaosEnabled)
	if cfg.ChaosEnabled {
		log.Printf("   - Chaos Interval: %ds", cfg.ChaosIntervalSeconds)
		log.Printf("   - Chaos Window: %ds", cfg.ChaosWindowSeconds)
	}
	log.Printf("📖 API documentation available at http://localhost:%s/", cfg.Port)

	if err := http.ListenAndServe(addr, handlerStack); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
