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

// LeaderboardEntry represents a single ranked entry in the leaderboard.
// Fields match the UI's ApiLeaderboardEntry contract after transformation in the Next.js route.
type LeaderboardEntry struct {
	TeamID         string  `json:"teamId"`
	Score          float64 `json:"score"`
	Name           string  `json:"name"`
	Color          string  `json:"color"`
	Solves         int     `json:"solves"`
	FastestSolveMs int64   `json:"fastestSolveMs"` // converted from nanoseconds stored in Redis
}

// updateTeamStatsScript is a Lua script that atomically updates all team state on a correct solve.
//
// KEYS[1] = team hash key              (e.g. "team:<teamID>")
// KEYS[2] = team solved set key        (e.g. "team:<teamID>:solved_characters")
// KEYS[3] = leaderboard sorted set key (e.g. "leaderboard")
// KEYS[4] = masterboard char set key   (e.g. "masterboard:<characterID>")
//
// ARGV[1] = score increment (integer, may be negative)
// ARGV[2] = character ID
// ARGV[3] = team ID
var updateTeamStatsScript = redis.NewScript(`
redis.call('HINCRBY', KEYS[1], 'score', ARGV[1])
redis.call('HINCRBY', KEYS[1], 'solves', 1)
redis.call('SADD', KEYS[2], ARGV[2])
redis.call('ZINCRBY', KEYS[3], ARGV[1], ARGV[3])
redis.call('SADD', KEYS[4], ARGV[3])
return 1
`)

// updateFastestSolveScript conditionally sets fastest_solve only when the new value is smaller.
//
// KEYS[1] = team hash key
// ARGV[1] = new elapsed nanoseconds (integer string)
var updateFastestSolveScript = redis.NewScript(`
local cur = redis.call('HGET', KEYS[1], 'fastest_solve_ns')
if cur == false or cur == '' or tonumber(ARGV[1]) < tonumber(cur) then
    redis.call('HSET', KEYS[1], 'fastest_solve_ns', ARGV[1])
    return 1
end
return 0
`)

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
	// Prefer fastest_solve_ns (integer nanoseconds written by the Lua script) when present
	// and non-zero; fall back to the human-readable fastest_solve string otherwise.
	if nsStr := teamData["fastest_solve_ns"]; nsStr != "" {
		if ns, err := strconv.ParseInt(nsStr, 10, 64); err == nil && ns > 0 {
			data.FastestSolve = time.Duration(ns)
		}
	} else if fastestSolve, err := time.ParseDuration(teamData["fastest_solve"]); err == nil {
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

// InitializeMasterboard sets up the initial state of the Masterboard in Redis.
// It iterates through the loaded character IDs and sets each field in the Hash to an empty JSON array `[]`.
// It only initializes if the Masterboard doesn't already exist.
func (s *Store) InitializeMasterboard(ctx context.Context, characterIDs []string) error {
	exists, err := s.client.Exists(ctx, "masterboard").Result()
	if err != nil {
		return err
	}

	if exists > 0 {
		// Masterboard already exists, do not overwrite
		return nil
	}

	pipe := s.client.Pipeline()
	for _, id := range characterIDs {
		pipe.HSet(ctx, "masterboard", id, "[]")
	}

	_, err = pipe.Exec(ctx)
	return err
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

// UpdateTeamStatsAtomic atomically updates all relevant data structures on a correct solve
// using a single Lua script, preventing race conditions between concurrent requests.
func (s *Store) UpdateTeamStatsAtomic(ctx context.Context, teamID, characterID string, scoreIncrement int) error {
	teamKey := "team:" + teamID
	solvedKey := "team:" + teamID + ":solved_characters"
	leaderboardKey := "leaderboard"
	masterboardKey := "masterboard:" + characterID

	return updateTeamStatsScript.Run(ctx, s.client,
		[]string{teamKey, solvedKey, leaderboardKey, masterboardKey},
		scoreIncrement, characterID, teamID,
	).Err()
}

// IncrementTeamScore updates the team's score in the hash and the leaderboard sorted set.
// Used for score adjustments that do not involve a correct solve (e.g. wrong-guess penalties).
func (s *Store) IncrementTeamScore(ctx context.Context, teamID string, scoreIncrement int) error {
	teamKey := "team:" + teamID
	pipe := s.client.Pipeline()
	pipe.HIncrBy(ctx, teamKey, "score", int64(scoreIncrement))
	pipe.ZIncrBy(ctx, "leaderboard", float64(scoreIncrement), teamID)
	_, err := pipe.Exec(ctx)
	return err
}

// ClearActiveSession clears the active_session_id field for a team.
func (s *Store) ClearActiveSession(ctx context.Context, teamID string) error {
	return s.client.HSet(ctx, "team:"+teamID, "active_session_id", "").Err()
}

// UpdateFastestSolve conditionally updates the fastest solve time using a Lua script
// so the comparison and write are atomic.
func (s *Store) UpdateFastestSolve(ctx context.Context, teamID string, elapsed time.Duration) error {
	teamKey := "team:" + teamID
	ns := strconv.FormatInt(elapsed.Nanoseconds(), 10)
	return updateFastestSolveScript.Run(ctx, s.client, []string{teamKey}, ns).Err()
}

// GetLeaderboardEntries fetches the full leaderboard from the sorted set in descending score
// order, then pipelines team detail lookups (name, color, solves, fastest_solve_ns) in a
// single round-trip to avoid N+1 queries.
func (s *Store) GetLeaderboardEntries(ctx context.Context) ([]LeaderboardEntry, error) {
	// ZREVRANGE with scores returns members ordered highest-score first.
	zResults, err := s.client.ZRevRangeWithScores(ctx, "leaderboard", 0, -1).Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}

	if len(zResults) == 0 {
		return []LeaderboardEntry{}, nil
	}

	// Pipeline all four HGET calls per team in a single round-trip.
	pipe := s.client.Pipeline()
	nameCmds := make([]*redis.StringCmd, len(zResults))
	colorCmds := make([]*redis.StringCmd, len(zResults))
	solvesCmds := make([]*redis.StringCmd, len(zResults))
	fastestCmds := make([]*redis.StringCmd, len(zResults))
	for i, z := range zResults {
		teamKey := "team:" + z.Member.(string)
		nameCmds[i] = pipe.HGet(ctx, teamKey, "name")
		colorCmds[i] = pipe.HGet(ctx, teamKey, "color")
		solvesCmds[i] = pipe.HGet(ctx, teamKey, "solves")
		fastestCmds[i] = pipe.HGet(ctx, teamKey, "fastest_solve_ns")
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	entries := make([]LeaderboardEntry, len(zResults))
	for i, z := range zResults {
		name, _ := nameCmds[i].Result()
		color, _ := colorCmds[i].Result()

		var solves int
		if solvesStr, err := solvesCmds[i].Result(); err == nil {
			solves, _ = strconv.Atoi(solvesStr)
		}

		var fastestSolveMs int64
		if nsStr, err := fastestCmds[i].Result(); err == nil {
			if ns, err := strconv.ParseInt(nsStr, 10, 64); err == nil && ns > 0 {
				fastestSolveMs = ns / int64(time.Millisecond)
			}
		}

		entries[i] = LeaderboardEntry{
			TeamID:         z.Member.(string),
			Score:          z.Score,
			Name:           name,
			Color:          color,
			Solves:         solves,
			FastestSolveMs: fastestSolveMs,
		}
	}
	return entries, nil
}

// GetTeamColors fetches the color field for a batch of team IDs using a single pipeline,
// avoiding N+1 round-trips when building the masterboard response.
func (s *Store) GetTeamColors(ctx context.Context, teamIDs []string) (map[string]string, error) {
	if len(teamIDs) == 0 {
		return map[string]string{}, nil
	}

	pipe := s.client.Pipeline()
	cmds := make([]*redis.StringCmd, len(teamIDs))
	for i, id := range teamIDs {
		cmds[i] = pipe.HGet(ctx, "team:"+id, "color")
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	result := make(map[string]string, len(teamIDs))
	for i, id := range teamIDs {
		if color, err := cmds[i].Result(); err == nil {
			result[id] = color
		}
	}
	return result, nil
}

// GetMasterboardFromSets reads the masterboard by fetching all character IDs from the
// initialised "masterboard" hash (for the full character list) and then reading each
// "masterboard:<characterID>" set that the Lua script populates.
func (s *Store) GetMasterboardFromSets(ctx context.Context) (map[string][]string, error) {
	// Get all character IDs that were registered at startup.
	charIDs, err := s.client.HKeys(ctx, "masterboard").Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}

	if len(charIDs) == 0 {
		return map[string][]string{}, nil
	}

	// Pipeline SMEMBERS for each character's set.
	pipe := s.client.Pipeline()
	memberCmds := make([]*redis.StringSliceCmd, len(charIDs))
	for i, id := range charIDs {
		memberCmds[i] = pipe.SMembers(ctx, "masterboard:"+id)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	result := make(map[string][]string, len(charIDs))
	for i, id := range charIDs {
		members, _ := memberCmds[i].Result()
		if members == nil {
			members = []string{}
		}
		result[id] = members
	}
	return result, nil
}