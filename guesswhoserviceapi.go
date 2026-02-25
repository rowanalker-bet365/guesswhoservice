package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/guesswho/config"
	"github.com/guesswho/internal/db"
	"github.com/guesswho/internal/handler"
	custommiddleware "github.com/guesswho/internal/middleware"
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
		Password: "",
		DB:       0, // use default DB
	})

	// Ping Redis to check the connection
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if _, err := redisClient.Ping(pingCtx).Result(); err != nil {
		log.Printf("Failed to connect to Redis at %s. Error: %v", cfg.RedisAddr, err)
		log.Fatalf("Exiting due to Redis connection failure.")
	}
	log.Println("✅ Successfully connected to Redis")

	// Initialize repositories
	dbStore := db.NewStore(redisClient)

	// Initialize services
	traitCatalog := service.NewTraitCatalogService()

	// Load Character Catalog
	characterCatalog, err := service.NewCharacterCatalogService("data/characters.json")
	if err != nil {
		log.Fatalf("Failed to load character catalog: %v", err)
	}
	log.Printf("✅ Successfully loaded %d characters", len(characterCatalog.GetAllCharacters()))

	// Initialize Masterboard using a fresh background context (not the ping timeout context).
	var charIDs []string
	for _, char := range characterCatalog.GetAllCharacters() {
		charIDs = append(charIDs, char.CandidateID)
	}
	if err := dbStore.InitializeMasterboard(context.Background(), charIDs); err != nil {
		log.Fatalf("Failed to initialize masterboard: %v", err)
	}
	log.Println("✅ Masterboard initialized (or already exists)")

	boardGenerator := service.NewBoardGeneratorService()
	encryptionService := service.NewEncryptionService()
	chaosService := service.NewChaosService(cfg.ChaosEnabled)
	scoringService := service.NewScoringService()
	milestoneService := service.NewMilestoneService(dbStore)

	sessionService := service.NewSessionService(
		dbStore,
		traitCatalog,
		boardGenerator,
		encryptionService,
		chaosService,
		scoringService,
		milestoneService,
		service.SessionServiceConfig{
			ChaosEnabled:  cfg.ChaosEnabled,
			ChaosInterval: cfg.ChaosIntervalSeconds,
			ChaosWindow:   cfg.ChaosWindowSeconds,
		},
	)

	// Initialize handlers
	sessionHandler := handler.NewSessionHandler(sessionService, traitCatalog)
	traitHandler := handler.NewTraitHandler(traitCatalog, encryptionService, milestoneService)
	clientHandler := handler.NewClientHandler(dbStore, encryptionService, characterCatalog, cfg.JWTSecret)
	debugAPIKey := cfg.DebugAPIKey
	debugHandler := handler.NewDebugHandler(dbStore, debugAPIKey)

	// Initialize middleware
	rateLimiter := custommiddleware.NewRateLimiter(cfg.RateLimitEnabled)
	jwtAuthMiddleware := custommiddleware.JWTAuth(cfg.JWTSecret)

	// Setup mux with Go 1.22+ patterns
	mux := http.NewServeMux()

	// --- Router Setup ---
	// Main router for public endpoints
	publicMux := mux

	// --- Client Routes ---

	// Public client routes (No JWT required)
	publicMux.HandleFunc("POST /client/auth/signup", clientHandler.SignupHandler)
	publicMux.HandleFunc("POST /client/auth/login", clientHandler.LoginHandler)
	publicMux.HandleFunc("GET /client/game/leaderboard", clientHandler.GetLeaderboard)
	publicMux.HandleFunc("GET /client/game/master-board", clientHandler.GetMasterBoardHandler)

	// Protected client routes (JWT required)
	protectedClientMux := http.NewServeMux()
	protectedClientMux.HandleFunc("GET /team/progress", clientHandler.GetTeamProgressHandler)
	protectedClientMux.HandleFunc("POST /team/reset", clientHandler.ResetTeamHandler)

	// Mount the authenticated router with middleware
	// Note: Specific routes like /client/auth/signup defined on 'publicMux' take precedence over this prefix match
	publicMux.Handle("/client/", http.StripPrefix("/client", jwtAuthMiddleware(protectedClientMux)))

	// Public API routes
	publicMux.Handle("POST /sessions/start", rateLimiter.Limit(10, 1)(http.HandlerFunc(sessionHandler.StartSession)))
	publicMux.HandleFunc("GET /sessions/{sessionId}/questions", traitHandler.GetQuestions)
	publicMux.HandleFunc("GET /sessions/{sessionId}/status", sessionHandler.Status)
	publicMux.Handle("POST /sessions/{sessionId}/ask", rateLimiter.Limit(60, 5)(http.HandlerFunc(sessionHandler.AskQuestion)))
	publicMux.HandleFunc("GET /sessions/{sessionId}/board", sessionHandler.GetBoard)
	publicMux.HandleFunc("POST /sessions/{sessionId}/decode", traitHandler.Decode)
	publicMux.HandleFunc("POST /sessions/{sessionId}/guess", sessionHandler.SubmitGuess)
	publicMux.HandleFunc("POST /sessions/{sessionId}/reveal", sessionHandler.Reveal)

	// Debug routes
	mux.HandleFunc("GET /debug/team/{teamId}", debugHandler.GetTeamDebug)
	mux.HandleFunc("POST /debug/flush", debugHandler.FlushAll)

	// Health check endpoint
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	// Root endpoint with API info
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"service": "Guess Who API",
			"version": "1.0.0",
			"endpoints": {
				"start_session": "POST /sessions/start",
				"get_board": "GET /sessions/{sessionId}/board",
				"get_questions": "GET /sessions/{sessionId}/questions",
				"session_status": "GET /sessions/{sessionId}/status",
				"ask_question": "POST /sessions/{sessionId}/ask",
				"decode": "POST /sessions/{sessionId}/decode",
				"submit_guess": "POST /sessions/{sessionId}/guess",
				"reveal": "POST /sessions/{sessionId}/reveal",
				"leaderboard": "GET /leaderboard",
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
