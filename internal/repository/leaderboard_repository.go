package repository

import (
	"sort"
	"sync"

	"github.com/guesswho/internal/domain"
)

// LeaderboardRepository manages team statistics
type LeaderboardRepository interface {
	RecordSolve(teamID string, timeSeconds int, questionsAsked int, score int, correct bool)
	GetLeaderboard() []domain.LeaderboardEntry
}

type inMemoryLeaderboardRepository struct {
	mu    sync.RWMutex
	teams map[string]*teamStats
}

type teamStats struct {
	TeamID         string
	TotalSolves    int
	CorrectSolves  int
	TotalTime      int
	TotalQuestions int
	TotalScore     int
	CurrentStreak  int
	BestStreak     int
}

// NewInMemoryLeaderboardRepository creates a new in-memory leaderboard repository
func NewInMemoryLeaderboardRepository() LeaderboardRepository {
	return &inMemoryLeaderboardRepository{
		teams: make(map[string]*teamStats),
	}
}

func (r *inMemoryLeaderboardRepository) RecordSolve(teamID string, timeSeconds int, questionsAsked int, score int, correct bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	stats, exists := r.teams[teamID]
	if !exists {
		stats = &teamStats{
			TeamID: teamID,
		}
		r.teams[teamID] = stats
	}

	stats.TotalSolves++
	stats.TotalTime += timeSeconds
	stats.TotalQuestions += questionsAsked

	if correct {
		stats.CorrectSolves++
		stats.TotalScore += score
		stats.CurrentStreak++
		if stats.CurrentStreak > stats.BestStreak {
			stats.BestStreak = stats.CurrentStreak
		}
	} else {
		stats.CurrentStreak = 0
	}
}

func (r *inMemoryLeaderboardRepository) GetLeaderboard() []domain.LeaderboardEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries := make([]domain.LeaderboardEntry, 0, len(r.teams))

	for _, stats := range r.teams {
		var avgTime, avgQuestions float64
		var successRate float64

		if stats.TotalSolves > 0 {
			avgTime = float64(stats.TotalTime) / float64(stats.TotalSolves)
			avgQuestions = float64(stats.TotalQuestions) / float64(stats.TotalSolves)
			successRate = float64(stats.CorrectSolves) / float64(stats.TotalSolves)
		}

		entry := domain.LeaderboardEntry{
			TeamID:         stats.TeamID,
			Solves:         stats.CorrectSolves,
			AvgTimeSeconds: avgTime,
			AvgQuestions:   avgQuestions,
			TotalScore:     stats.TotalScore,
			SuccessRate:    successRate,
			BestStreak:     stats.BestStreak,
		}
		entries = append(entries, entry)
	}

	// Sort by total score descending
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].TotalScore > entries[j].TotalScore
	})

	return entries
}
