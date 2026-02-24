package db

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/guesswho/internal/domain"
)

// TeamData represents the unified data model for a team.
type TeamData struct {
	Name             string
	PasswordHash     string
	RegisteredAt     time.Time
	Color            string
	Score            int
	Solves           int
	FastestSolve     time.Duration
	SolvedCharacters []string
	Milestones       []string
	ActiveSessionID  string
}

// Store handles all Redis data interactions.
type Store struct {
	client *redis.Client
}

// NewStore creates a new Store.
func NewStore(client *redis.Client) *Store {
	return &Store{client: client}
}

// WriteTeamData saves the TeamData to Redis.
func (s *Store) WriteTeamData(ctx context.Context, teamID string, data *TeamData) error {
	teamKey := "team:" + teamID
	pipe := s.client.Pipeline()

	// Add team name to the name-to-ID mapping.
	pipe.HSet(ctx, "team_names_to_ids", data.Name, teamID)

	// Add team ID to the set of all team IDs.
	pipe.SAdd(ctx, "all_team_ids", teamID)

	pipe.HSet(ctx, teamKey,
		"name", data.Name,
		"password_hash", data.PasswordHash,
		"registered_at", data.RegisteredAt.Format(time.RFC3339Nano),
		"color", data.Color,
		"score", data.Score,
		"solves", data.Solves,
		"fastest_solve", data.FastestSolve.String(),
		"milestones", strings.Join(data.Milestones, ","),
		"active_session_id", data.ActiveSessionID,
	)

	solvedCharsKey := "team:" + teamID + ":solved_characters"
	// First, remove all existing solved characters to ensure a clean slate
	pipe.Del(ctx, solvedCharsKey)
	if len(data.SolvedCharacters) > 0 {
		// Convert []string to []interface{} for SAdd
		sChars := make([]interface{}, len(data.SolvedCharacters))
		for i, v := range data.SolvedCharacters {
			sChars[i] = v
		}
		pipe.SAdd(ctx, solvedCharsKey, sChars...)
	}

	_, err := pipe.Exec(ctx)
	return err
}

// ReadTeamData reads all the data for a given teamID from Redis.
func (s *Store) ReadTeamData(ctx context.Context, teamID string) (*TeamData, error) {
	teamKey := "team:" + teamID
	solvedCharsKey := "team:" + teamID + ":solved_characters"

	var data TeamData
	pipe := s.client.Pipeline()

	teamDataCmd := pipe.HGetAll(ctx, teamKey)
	solvedCharsCmd := pipe.SMembers(ctx, solvedCharsKey)

	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	teamData, err := teamDataCmd.Result()
	if err == redis.Nil {
		// If the hash doesn't exist, it's not an error, just means no data.
		// We can return an empty TeamData struct or a specific error.
		// For now, let's return nil and a "not found" error.
		return nil, redis.Nil // Or a custom not found error
	}
	if err != nil {
		return nil, err
	}

	data.Name = teamData["name"]
	data.PasswordHash = teamData["password_hash"]
	if registeredAt, err := time.Parse(time.RFC3339Nano, teamData["registered_at"]); err == nil {
		data.RegisteredAt = registeredAt
	}
	data.Color = teamData["color"]
	if score, err := strconv.Atoi(teamData["score"]); err == nil {
		data.Score = score
	}
	if solves, err := strconv.Atoi(teamData["solves"]); err == nil {
		data.Solves = solves
	}
	if fastestSolve, err := time.ParseDuration(teamData["fastest_solve"]); err == nil {
		data.FastestSolve = fastestSolve
	}
	if milestonesStr := teamData["milestones"]; milestonesStr != "" {
		data.Milestones = strings.Split(milestonesStr, ",")
	}
	data.ActiveSessionID = teamData["active_session_id"]

	solvedChars, err := solvedCharsCmd.Result()
	// SMembers doesn't return redis.Nil if the key doesn't exist, but an empty slice.
	if err != nil {
		return nil, err
	}
	data.SolvedCharacters = solvedChars

	return &data, nil
}

// GetTeamIDByName looks up a team ID by its name.
func (s *Store) GetTeamIDByName(ctx context.Context, name string) (string, error) {
	return s.client.HGet(ctx, "team_names_to_ids", name).Result()
}

// GetAllTeamIDs returns all team IDs from the set.
func (s *Store) GetAllTeamIDs(ctx context.Context) ([]string, error) {
	return s.client.SMembers(ctx, "all_team_ids").Result()
}

// WriteSession saves the entire Session object to Redis as a JSON string.
func (s *Store) WriteSession(ctx context.Context, session *domain.Session) error {
	key := "session:" + session.SessionID
	val, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, key, val, 24*time.Hour).Err()
}

// ReadSession retrieves and deserializes the Session object from Redis.
func (s *Store) ReadSession(ctx context.Context, sessionID string) (*domain.Session, error) {
	key := "session:" + sessionID
	val, err := s.client.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	var session domain.Session
	if err := json.Unmarshal([]byte(val), &session); err != nil {
		return nil, err
	}
	return &session, nil
}

// PublishGameUpdate publishes a game update event to Redis.
func (s *Store) PublishGameUpdate(ctx context.Context, teamID, characterID string) error {
	payload := map[string]string{
		"teamId":      teamID,
		"characterId": characterID,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	return s.client.Publish(ctx, "game_updates", jsonPayload).Err()
}