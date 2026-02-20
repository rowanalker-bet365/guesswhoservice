package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/guesswho/config"
	"github.com/guesswho/internal/handler"
	custommiddleware "github.com/guesswho/internal/middleware"
	"github.com/guesswho/internal/repository"
	"github.com/guesswho/internal/service"
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
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Team-Id, X-Api-Key, Authorization")
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

	// Initialize Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       0, // use default DB
	})

	// Ping Redis to check the connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := redisClient.Ping(ctx).Result(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("✅ Successfully connected to Redis")

	// Initialize repositories
	sessionRepo := repository.NewRedisSessionRepository(redisClient)
	leaderboardRepo := repository.NewRedisLeaderboardRepository(redisClient)

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
	clientHandler := handler.NewClientHandler(sessionService, leaderboardRepo, traitCatalog, encryptionService, cfg.JWTSecret)

	// Initialize middleware
	rateLimiter := custommiddleware.NewRateLimiter(cfg.RateLimitEnabled)
	jwtAuthMiddleware := custommiddleware.JWTAuth(cfg.JWTSecret)

	// Setup mux with Go 1.22+ patterns
	mux := http.NewServeMux()
	
	// --- Router Setup ---
	// Main router for public endpoints
	publicMux := mux

	// Router for client-facing authenticated endpoints (JWT)
	clientMux := http.NewServeMux()
	clientMux.HandleFunc("GET /v1/team/progress", clientHandler.GetTeamProgressHandler)
	clientMux.HandleFunc("GET /v1/sessions/{sessionId}/board", clientHandler.GetBoardHandler)
	clientMux.HandleFunc("GET /v1/leaderboard", clientHandler.GetLeaderboardHandler)

	// --- Route Registration ---
	// Mount the authenticated routers with their respective middleware
	publicMux.Handle("/client/", http.StripPrefix("/client", jwtAuthMiddleware(clientMux)))

	// Public API routes
	publicMux.HandleFunc("POST /v1/auth/signup", clientHandler.SignupHandler)
	publicMux.HandleFunc("POST /v1/auth/login", clientHandler.LoginHandler)
	publicMux.Handle("POST /v1/sessions/start", rateLimiter.Limit(10, 1)(http.HandlerFunc(sessionHandler.StartSession)))
	publicMux.HandleFunc("GET /v1/sessions/{sessionId}/questions", traitHandler.GetQuestions)
	publicMux.HandleFunc("GET /v1/sessions/{sessionId}/status", sessionHandler.Status)
	publicMux.Handle("POST /v1/sessions/{sessionId}/ask", rateLimiter.Limit(60, 5)(http.HandlerFunc(sessionHandler.AskQuestion)))
	publicMux.HandleFunc("POST /v1/sessions/{sessionId}/decode", traitHandler.Decode)
	publicMux.HandleFunc("POST /v1/sessions/{sessionId}/guess", sessionHandler.SubmitGuess)
	publicMux.HandleFunc("POST /v1/sessions/{sessionId}/reveal", sessionHandler.Reveal)


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
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080" // A default for local running
	}

	log.Printf("🚀 Starting Guess Who API server, listening on port %s", port)
	log.Printf("⚙️  Configuration:")
	log.Printf("   - Rate Limiting: %v", cfg.RateLimitEnabled)
	log.Printf("   - Chaos Mode: %v", cfg.ChaosEnabled)
	if cfg.ChaosEnabled {
		log.Printf("   - Chaos Interval: %ds", cfg.ChaosIntervalSeconds)
		log.Printf("   - Chaos Window: %ds", cfg.ChaosWindowSeconds)
	}
	log.Printf("📖 API documentation available at http://localhost:%s/", port)

	log.Fatal(http.ListenAndServe(":"+port, handlerStack))
}
