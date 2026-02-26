package service

import (
	"context"

	"github.com/go-redis/redis/v8"
	"github.com/guesswho/internal/db"
	"github.com/guesswho/internal/domain"
	"github.com/guesswho/internal/logging"
)

// MilestoneService handles milestone checking and awarding.
type MilestoneService interface {
	// AwardIfAbsent checks whether teamID already holds the given milestone.
	// If the team does not yet have it, the milestone is appended to their list,
	// persisted via a targeted HSET, and the corresponding score bonus is applied
	// via IncrementTeamScore (HINCRBY + ZINCRBY leaderboard).
	// Returns true if the milestone was newly awarded, false if already present.
	// All errors are logged but never propagated so milestone failures cannot
	// block the main game flow.
	AwardIfAbsent(ctx context.Context, teamID string, milestone domain.Milestone) bool
}

type milestoneService struct {
	store *db.Store
}

// NewMilestoneService creates a new MilestoneService backed by the given store.
func NewMilestoneService(store *db.Store) MilestoneService {
	return &milestoneService{store: store}
}

func (m *milestoneService) AwardIfAbsent(ctx context.Context, teamID string, milestone domain.Milestone) bool {
	// Look up the score bonus for this milestone.
	scoreBonus, ok := domain.MilestoneScoreMap[milestone]
	if !ok {
		logging.Warn(ctx, "unknown milestone", "milestone", milestone)
		return false
	}

	// Read current team data to inspect the existing milestones list.
	teamData, err := m.store.ReadTeamData(ctx, teamID)
	if err != nil && err != redis.Nil {
		logging.Error(ctx, "failed to read team data for milestone check", "error", err)
		return false
	}
	if teamData == nil {
		logging.Warn(ctx, "no team data found for milestone", "milestone", milestone)
		return false
	}

	// Idempotency check: bail out if the milestone is already in the list.
	milestoneStr := string(milestone)
	for _, existing := range teamData.Milestones {
		if existing == milestoneStr {
			return false // already awarded, no-op
		}
	}

	// Append and persist the updated milestones list.
	updatedMilestones := append(teamData.Milestones, milestoneStr)
	if err := m.store.SetMilestones(ctx, teamID, updatedMilestones); err != nil {
		logging.Error(ctx, "failed to persist milestone", "milestone", milestone, "error", err)
		return false
	}

	// Award the score bonus using the existing IncrementTeamScore pipeline
	// (HINCRBY on team hash + ZINCRBY on the leaderboard sorted set).
	if err := m.store.IncrementTeamScore(ctx, teamID, scoreBonus); err != nil {
		// The milestone record was already written above; log the score failure
		// but return true so callers know the milestone was recorded.
		logging.Error(ctx, "milestone recorded but score increment failed", "milestone", milestone, "error", err)
		return true
	}

	logging.Info(ctx, "milestone awarded", "milestone", milestone, "scoreBonus", scoreBonus)
	return true
}
