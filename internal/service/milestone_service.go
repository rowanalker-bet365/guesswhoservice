package service

import (
	"context"

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
	scoreBonus, ok := domain.MilestoneScoreMap[milestone]
	if !ok {
		logging.Warn(ctx, "unknown milestone", "milestone", milestone)
		return false
	}

	awarded, err := m.store.AwardMilestoneAtomic(ctx, teamID, string(milestone), scoreBonus)
	if err != nil {
		logging.Error(ctx, "failed to award milestone atomically", "milestone", milestone, "error", err)
		return false
	}

	if awarded {
		logging.Info(ctx, "milestone awarded", "milestone", milestone, "scoreBonus", scoreBonus)
	}
	return awarded
}
