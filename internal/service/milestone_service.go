package service

import (
	"context"
	"log"

	"github.com/go-redis/redis/v8"
	"github.com/guesswho/internal/db"
	"github.com/guesswho/internal/domain"
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
		log.Printf("[milestone] Unknown milestone %q for team %s — skipping", milestone, teamID)
		return false
	}

	// Read current team data to inspect the existing milestones list.
	teamData, err := m.store.ReadTeamData(ctx, teamID)
	if err != nil && err != redis.Nil {
		log.Printf("[milestone] Error reading team data for team %s: %v", teamID, err)
		return false
	}
	if teamData == nil {
		log.Printf("[milestone] No team data found for team %s — skipping milestone %s", teamID, milestone)
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
		log.Printf("[milestone] Error persisting milestone %s for team %s: %v", milestone, teamID, err)
		return false
	}

	// Award the score bonus using the existing IncrementTeamScore pipeline
	// (HINCRBY on team hash + ZINCRBY on the leaderboard sorted set).
	if err := m.store.IncrementTeamScore(ctx, teamID, scoreBonus); err != nil {
		// The milestone record was already written above; log the score failure
		// but return true so callers know the milestone was recorded.
		log.Printf("[milestone] Milestone %s recorded for team %s but score increment failed: %v", milestone, teamID, err)
		return true
	}

	log.Printf("[milestone] ✅ Awarded %s (+%d pts) to team %s", milestone, scoreBonus, teamID)
	return true
}
