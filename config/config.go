package config

import (
	"os"
	"strconv"
)

// Config holds application configuration
type Config struct {
	Port                 string
	RateLimitEnabled     bool
	ChaosEnabled         bool
	ChaosIntervalSeconds int
	ChaosWindowSeconds   int
	RedisAddr            string
	RedisPassword        string
	ServiceURL           string
	JWTSecret            string
	DebugAPIKey          string
	LogLevel             string
	GCPProjectID         string
}

// Load loads configuration from environment variables
func Load() *Config {
	return &Config{
		Port:                 getEnv("PORT", "8080"),
		RateLimitEnabled:     getEnvBool("RATE_LIMIT_ENABLED", false),
		ChaosEnabled:         getEnvBool("CHAOS_ENABLED", false),
		ChaosIntervalSeconds: getEnvInt("CHAOS_INTERVAL_SECONDS", 240),
		ChaosWindowSeconds:   getEnvInt("CHAOS_WINDOW_SECONDS", 90),
		RedisAddr:            getEnv("REDIS_ADDR", "localhost:6379"),
		ServiceURL:           getEnv("GUESSWHOSERVICE_URL", ""),
		JWTSecret:            getEnv("JWT_SECRET", "a-very-secret-key"),
		DebugAPIKey:          os.Getenv("DEBUG_API_KEY"),
		LogLevel:             getEnv("LOG_LEVEL", "info"),
		GCPProjectID:         getEnv("GCP_PROJECT_ID", "verificationguesswho"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}
