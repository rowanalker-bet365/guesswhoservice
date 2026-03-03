package db

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
if cur == false or cur == '' or tonumber(cur) == 0 or tonumber(ARGV[1]) < tonumber(cur) then
    redis.call('HSET', KEYS[1], 'fastest_solve_ns', ARGV[1])
    return 1
end
return 0
`)

// releaseSessionLockScript safely releases a session lock only if the caller still holds it.
//
// KEYS[1] = lock key
// ARGV[1] = lock value (must match the value set during acquisition)
var releaseSessionLockScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
    return redis.call('DEL', KEYS[1])
else
    return 0
end
`)

// awardMilestoneScript atomically checks whether a milestone already exists for a team
// and awards it (appending to the comma-separated milestones field, incrementing score,
// and updating the leaderboard) only if the team does not already hold it.
//
// KEYS[1] = team hash key       (e.g. "team:<teamID>")
// KEYS[2] = leaderboard sorted set key (e.g. "leaderboard")
//
// ARGV[1] = milestone string    (e.g. "M1")
// ARGV[2] = score bonus         (integer)
// ARGV[3] = team ID             (for leaderboard ZINCRBY member)
//
// Returns 1 if newly awarded, 0 if already present.
var awardMilestoneScript = redis.NewScript(`
local milestones = redis.call('HGET', KEYS[1], 'milestones')
local milestone = ARGV[1]
local bonus = tonumber(ARGV[2])
local teamID = ARGV[3]

if milestones and milestones ~= '' then
    for m in string.gmatch(milestones, '[^,]+') do
        if m == milestone then
            return 0
        end
    end
    milestones = milestones .. ',' .. milestone
else
    milestones = milestone
end

redis.call('HSET', KEYS[1], 'milestones', milestones)
redis.call('HINCRBY', KEYS[1], 'score', bonus)
redis.call('ZINCRBY', KEYS[2], bonus, teamID)
return 1
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
	client       *redis.Client
	characterIDs []string
}

// NewStore creates a new Store.
// characterIDs is the full list of character IDs loaded from the catalog at startup;
// it is stored so that InitializeMasterboard can be called at any time without
// needing the caller to supply the list again (e.g. after a debug flush).
func NewStore(client *redis.Client, characterIDs []string) *Store {
	return &Store{client: client, characterIDs: characterIDs}
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

// InitializeMasterboard ensures the "masterboard" HASH contains an entry for every
// known character ID. It is idempotent: HSetNX only writes a field when it does not
// already exist, so calling this on an already-populated board is a safe no-op for
// existing entries while filling in any that are missing.
//
// This design means the function is safe to call at startup, after a debug flush,
// or from GetMasterboardFromSets when self-healing a missing HASH.
func (s *Store) InitializeMasterboard(ctx context.Context) error {
	if len(s.characterIDs) == 0 {
		return nil
	}
	pipe := s.client.Pipeline()
	for _, id := range s.characterIDs {
		pipe.HSetNX(ctx, "masterboard", id, "[]")
	}
	_, err := pipe.Exec(ctx)
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

// SetActiveSession sets the active_session_id field for a team to the given session ID.
// This is a targeted HSET that does not touch any other team fields.
func (s *Store) SetActiveSession(ctx context.Context, teamID, sessionID string) error {
	return s.client.HSet(ctx, "team:"+teamID, "active_session_id", sessionID).Err()
}

// ClearSolvedCharacters removes all members from the team's solved-characters set.
// Used when a team has solved all characters and needs to start fresh.
func (s *Store) ClearSolvedCharacters(ctx context.Context, teamID string) error {
	return s.client.Del(ctx, "team:"+teamID+":solved_characters").Err()
}

// SetMilestones updates only the milestones field in the team hash.
// This is a targeted HSET that does not touch any other team fields,
// analogous to SetActiveSession. The milestones slice is stored as a
// comma-separated string, matching the format used by WriteTeamData and ReadTeamData.
func (s *Store) SetMilestones(ctx context.Context, teamID string, milestones []string) error {
	return s.client.HSet(ctx, "team:"+teamID, "milestones", strings.Join(milestones, ",")).Err()
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

// GetMasterboardFromSets reads the masterboard by reading directly from the
// per-character "masterboard:<charID>" SETs that are populated atomically by the
// Lua script on each correct solve. This is the authoritative source of truth for
// which teams have solved which characters, and is not affected by team resets.
//
//  1. Reads all character IDs from the "masterboard" HASH (full character list, including unsolved).
//  2. Pipelines SMEMBERS masterboard:<charID> for every character.
//  3. Returns the map characterID -> []teamID, defaulting to an empty slice for unsolved characters.
func (s *Store) GetMasterboardFromSets(ctx context.Context) (map[string][]string, error) {
	// Step 1: Get all character IDs registered at startup.
	charIDs, err := s.client.HKeys(ctx, "masterboard").Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}

	// Self-heal: if the "masterboard" HASH is missing (e.g. after a debug flush),
	// re-initialize it from the stored character list and retry the lookup once.
	if len(charIDs) == 0 {
		if initErr := s.InitializeMasterboard(ctx); initErr == nil {
			charIDs, err = s.client.HKeys(ctx, "masterboard").Result()
			if err != nil && err != redis.Nil {
				return nil, err
			}
		}
	}

	if len(charIDs) == 0 {
		return map[string][]string{}, nil
	}

	// Initialise result with empty slices for every character.
	result := make(map[string][]string, len(charIDs))
	for _, id := range charIDs {
		result[id] = []string{}
	}

	// Step 2: Pipeline SMEMBERS masterboard:<charID> for every character.
	// These sets are written atomically by the Lua script on each correct solve
	// and are never cleared when a team resets their progress.
	pipe := s.client.Pipeline()
	solvedCmds := make([]*redis.StringSliceCmd, len(charIDs))
	for i, charID := range charIDs {
		solvedCmds[i] = pipe.SMembers(ctx, "masterboard:"+charID)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	// Step 3: Build map characterID -> []teamID.
	for i, charID := range charIDs {
		teams, _ := solvedCmds[i].Result()
		if len(teams) > 0 {
			result[charID] = teams
		}
	}

	return result, nil
}

// CountActiveSessions returns the number of session IDs currently tracked in the
// team's active-sessions set (key: "active_sessions:team:<teamID>").
func (s *Store) CountActiveSessions(ctx context.Context, teamID string) (int64, error) {
	key := "active_sessions:team:" + teamID
	return s.client.SCard(ctx, key).Result()
}

// AddActiveSession adds sessionID to the team's active-sessions set and refreshes
// the set's TTL to match the session TTL (24 h).
func (s *Store) AddActiveSession(ctx context.Context, teamID, sessionID string) error {
	key := "active_sessions:team:" + teamID
	pipe := s.client.Pipeline()
	pipe.SAdd(ctx, key, sessionID)
	pipe.Expire(ctx, key, 24*time.Hour)
	_, err := pipe.Exec(ctx)
	return err
}

// RemoveActiveSession removes sessionID from the team's active-sessions set.
// Called when a session ends (correct guess, reveal, or exhausted guesses).
func (s *Store) RemoveActiveSession(ctx context.Context, teamID, sessionID string) error {
	key := "active_sessions:team:" + teamID
	return s.client.SRem(ctx, key, sessionID).Err()
}

// FlushAll deletes every key in the current Redis database.
// Use this to wipe all data before a fresh hackathon run.
func (s *Store) FlushAll(ctx context.Context) error {
	return s.client.FlushDB(ctx).Err()
}

// AcquireSessionLock acquires a distributed lock for a session using SET NX EX.
// Returns the lock value needed to release the lock, or an error if the lock
// could not be acquired after retries.
func (s *Store) AcquireSessionLock(ctx context.Context, sessionID string) (string, error) {
	lockKey := "lock:session:" + sessionID
	b := make([]byte, 16)
	if _, err := cryptorand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate lock value: %w", err)
	}
	lockValue := hex.EncodeToString(b)

	for i := 0; i < 10; i++ {
		ok, err := s.client.SetNX(ctx, lockKey, lockValue, 5*time.Second).Result()
		if err != nil {
			return "", fmt.Errorf("failed to acquire session lock: %w", err)
		}
		if ok {
			return lockValue, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return "", fmt.Errorf("could not acquire session lock: contention on %s", sessionID)
}

// ReleaseSessionLock releases a session lock only if the caller still holds it.
func (s *Store) ReleaseSessionLock(ctx context.Context, sessionID string, lockValue string) {
	lockKey := "lock:session:" + sessionID
	releaseSessionLockScript.Run(ctx, s.client, []string{lockKey}, lockValue)
}

// AwardMilestoneAtomic atomically checks whether a team already holds a milestone
// and awards it (appending, incrementing score, updating leaderboard) only if absent.
// Returns true if newly awarded, false if already present.
func (s *Store) AwardMilestoneAtomic(ctx context.Context, teamID string, milestone string, scoreBonus int) (bool, error) {
	teamKey := "team:" + teamID
	leaderboardKey := "leaderboard"
	result, err := awardMilestoneScript.Run(ctx, s.client, []string{teamKey, leaderboardKey}, milestone, scoreBonus, teamID).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

// ResetTeamProgress clears the active session and solved characters for a team
// using targeted Redis operations that do not overwrite other team fields.
func (s *Store) ResetTeamProgress(ctx context.Context, teamID string) error {
	pipe := s.client.Pipeline()
	pipe.HSet(ctx, "team:"+teamID, "active_session_id", "")
	pipe.Del(ctx, "team:"+teamID+":solved_characters")
	_, err := pipe.Exec(ctx)
	return err
}

// RegisterTeamName atomically registers a team name using HSETNX.
// Returns true if the name was newly registered, false if it already exists.
func (s *Store) RegisterTeamName(ctx context.Context, teamName, teamID string) (bool, error) {
	return s.client.HSetNX(ctx, "team_names_to_ids", teamName, teamID).Result()
}

// TeamExists checks whether a team hash exists in Redis.
// Returns true if the key "team:<teamID>" is present, false otherwise.
func (s *Store) TeamExists(ctx context.Context, teamID string) (bool, error) {
	n, err := s.client.Exists(ctx, "team:"+teamID).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
