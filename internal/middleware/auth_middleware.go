package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/guesswho/internal/logging"
)

// contextKey is a private type to prevent collisions in context keys.
type contextKey string

// TeamIDKey is the key for the team ID in the request context.
const TeamIDKey contextKey = "teamId"

// JWTAuth is a middleware that validates client-facing JWTs.
func JWTAuth(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				logging.Warn(r.Context(), "missing Authorization header")
				http.Error(w, "Authorization header required", http.StatusUnauthorized)
				return
			}

			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				logging.Warn(r.Context(), "invalid Authorization header format")
				http.Error(w, "Invalid Authorization header format", http.StatusUnauthorized)
				return
			}
			tokenString := parts[1]

			token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
				}
				return []byte(jwtSecret), nil
			})

			if err != nil {
				logging.Warn(r.Context(), "JWT validation failed", "error", err)
				http.Error(w, "Invalid token: "+err.Error(), http.StatusUnauthorized)
				return
			}

			if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
				teamID, ok := claims["teamId"].(string)
				if !ok {
					logging.Warn(r.Context(), "JWT missing teamId claim")
					http.Error(w, "Invalid token: teamId claim is missing or not a string", http.StatusUnauthorized)
					return
				}
				logging.Debug(r.Context(), "JWT authenticated", "teamId", teamID)
				// Add teamId to context
				ctx := context.WithValue(r.Context(), TeamIDKey, teamID)
				next.ServeHTTP(w, r.WithContext(ctx))
			} else {
				logging.Warn(r.Context(), "invalid JWT token")
				http.Error(w, "Invalid token", http.StatusUnauthorized)
			}
		})
	}
}
