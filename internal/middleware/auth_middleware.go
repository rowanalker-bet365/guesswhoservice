package middleware

import (
	"context"
	"net/http"
	"strings"

	"fmt"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/api/idtoken"
)

// OIDCAuth is a middleware that validates Google-issued OIDC tokens.
// It checks the token's signature, issuer, and audience.
func OIDCAuth(audience string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "Authorization header required", http.StatusUnauthorized)
				return
			}

			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				http.Error(w, "Invalid Authorization header format", http.StatusUnauthorized)
				return
			}
			tokenString := parts[1]

			payload, err := idtoken.Validate(context.Background(), tokenString, audience)
			if err != nil {
				http.Error(w, "Invalid token: "+err.Error(), http.StatusUnauthorized)
				return
			}

			// The token is valid. For now, we don't use the payload, but we could
			// add it to the request context if needed in the future.
			_ = payload // Avoid "unused variable" compiler error

			next.ServeHTTP(w, r)
		})
	}
}

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
				http.Error(w, "Authorization header required", http.StatusUnauthorized)
				return
			}

			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
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
				http.Error(w, "Invalid token: "+err.Error(), http.StatusUnauthorized)
				return
			}

			if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
				teamID, ok := claims["teamId"].(string)
				if !ok {
					http.Error(w, "Invalid token: teamId claim is missing or not a string", http.StatusUnauthorized)
					return
				}
				// Add teamId to context
				ctx := context.WithValue(r.Context(), TeamIDKey, teamID)
				next.ServeHTTP(w, r.WithContext(ctx))
			} else {
				http.Error(w, "Invalid token", http.StatusUnauthorized)
			}
		})
	}
}