package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/guesswho/internal/domain"
)

type redisSessionRepository struct {
	client *redis.Client
}

func NewRedisSessionRepository(client *redis.Client) SessionRepository {
	return &redisSessionRepository{client: client}
}

func (r *redisSessionRepository) Save(session *domain.Session) error {
	ctx := context.Background()
	key := fmt.Sprintf("session:%s", session.SessionID)

	data, err := json.Marshal(session)
	if err != nil {
		return err
	}

	// Sessions expire after 1 hour of inactivity
	return r.client.Set(ctx, key, data, 1*time.Hour).Err()
}

func (r *redisSessionRepository) GetByID(sessionID string) (*domain.Session, error) {
	ctx := context.Background()
	key := fmt.Sprintf("session:%s", sessionID)

	data, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	} else if err != nil {
		return nil, err
	}

	var session domain.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}

	return &session, nil
}

func (r *redisSessionRepository) GetByTeamID(teamID string) []*domain.Session {
	// This is a very inefficient way to get sessions by team ID in Redis.
	// A better approach would be to maintain an index (e.g., a Set) of session IDs for each team.
	// For the purpose of this exercise, we will scan all session keys.
	// This is not recommended for production use.
	ctx := context.Background()
	var sessions []*domain.Session

	iter := r.client.Scan(ctx, 0, "session:*", 0).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		session, err := r.GetByID(key[len("session:"):]) // Strip the prefix
		if err != nil {
			continue
		}
		if session.TeamID == teamID {
			sessions = append(sessions, session)
		}
	}

	return sessions
}

func (r *redisSessionRepository) Delete(sessionID string) error {
	ctx := context.Background()
	key := fmt.Sprintf("session:%s", sessionID)
	return r.client.Del(ctx, key).Err()
}