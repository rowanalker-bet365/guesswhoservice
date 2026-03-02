package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/guesswho/config"
	"github.com/guesswho/internal/db"
	"github.com/guesswho/internal/handler"
	"github.com/guesswho/internal/logging"
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

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Team-Id, X-Api-Key, X-Key-Id, X-Debug-Key, Authorization")
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

	// Initialize structured logging
	logging.Init(cfg.LogLevel, cfg.GCPProjectID)
	logger := logging.NewLogger("startup")

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
		logger.Error("failed to connect to Redis", "addr", cfg.RedisAddr, "error", err)
		os.Exit(1)
	}
	logger.Info("connected to Redis", "addr", cfg.RedisAddr)

	// Initialize services
	traitCatalog := service.NewTraitCatalogService()

	// Load Character Catalog before creating the store so that character IDs
	// can be embedded in the store for idempotent masterboard initialization.
	characterCatalog, err := service.NewCharacterCatalogService("data/characters.json")
	if err != nil {
		logger.Error("failed to load character catalog", "error", err)
		os.Exit(1)
	}
	logger.Info("character catalog loaded", "count", len(characterCatalog.GetAllCharacters()))

	// Build the character ID list used to seed the masterboard HASH.
	var charIDs []string
	for _, char := range characterCatalog.GetAllCharacters() {
		charIDs = append(charIDs, char.CandidateID)
	}

	// Initialize repositories — pass charIDs so the store can re-initialize the
	// masterboard HASH at any time (e.g. after a debug flush) without needing the
	// caller to supply the list again.
	dbStore := db.NewStore(redisClient, charIDs)

	// Initialize Masterboard using a fresh background context (not the ping timeout context).
	if err := dbStore.InitializeMasterboard(context.Background()); err != nil {
		logger.Error("failed to initialize masterboard", "error", err)
		os.Exit(1)
	}
	logger.Info("masterboard initialized")

	boardGenerator := service.NewBoardGeneratorService()
	encryptionService := service.NewEncryptionService()
	chaosService := service.NewChaosService(cfg.ChaosEnabled, cfg.ChaosWindowSeconds, cfg.ChaosIntervalSeconds)
	scoringService := service.NewScoringService()
	milestoneService := service.NewMilestoneService(dbStore)

	sessionService := service.NewSessionService(
		dbStore,
		traitCatalog,
		characterCatalog,
		boardGenerator,
		encryptionService,
		chaosService,
		scoringService,
		milestoneService,
		service.SessionServiceConfig{
			ChaosInterval: cfg.ChaosIntervalSeconds,
			ChaosWindow:   cfg.ChaosWindowSeconds,
		},
	)

	// Initialize handlers
	sessionHandler := handler.NewSessionHandler(sessionService, traitCatalog)
	traitHandler := handler.NewTraitHandler(traitCatalog, milestoneService, sessionService, encryptionService)
	clientHandler := handler.NewClientHandler(dbStore, encryptionService, characterCatalog, cfg.JWTSecret)
	debugAPIKey := cfg.DebugAPIKey
	debugHandler := handler.NewDebugHandler(dbStore, debugAPIKey, encryptionService)
	chaosHandler := handler.NewChaosHandler(chaosService, debugAPIKey)

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
	publicMux.HandleFunc("POST /sessions/{sessionId}/guess", sessionHandler.SubmitGuess)
	publicMux.HandleFunc("POST /sessions/{sessionId}/reveal", sessionHandler.Reveal)
	publicMux.HandleFunc("POST /sessions/{sessionId}/decode", traitHandler.Decode)
	publicMux.HandleFunc("POST /sessions/{sessionId}/decrypt", sessionHandler.SubmitDecryption)

	// Debug routes
	mux.HandleFunc("GET /debug/team/{teamId}", debugHandler.GetTeamDebug)
	mux.HandleFunc("POST /debug/flush", debugHandler.FlushAll)
	mux.HandleFunc("POST /debug/decrypt", debugHandler.DecryptHandler)

	// Chaos trigger routes
	mux.HandleFunc("POST /chaos/trigger", chaosHandler.TriggerChaos)
	mux.HandleFunc("GET /chaos/status", chaosHandler.GetChaosStatus)

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
		logging.HTTPMiddleware,
		timeoutMiddleware(60*time.Second),
	)

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080" // A default for local running
	}

	logger.Info("starting server", "port", port)
	logger.Info("configuration loaded",
		"rateLimitEnabled", cfg.RateLimitEnabled,
		"chaosEnabled", cfg.ChaosEnabled,
		"chaosInterval", cfg.ChaosIntervalSeconds,
		"chaosWindow", cfg.ChaosWindowSeconds,
	)

	if err := http.ListenAndServe(":"+port, handlerStack); err != nil {
		logger.Error("server failed to start", "error", err)
		os.Exit(1)
	}
}
