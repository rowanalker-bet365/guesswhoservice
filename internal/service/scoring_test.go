package service

import (
	"testing"
	"time"

	"github.com/guesswho/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestScoringService_CalculateScore(t *testing.T) {
	service := NewScoringService()

	t.Run("calculates correct base score", func(t *testing.T) {
		session := &domain.Session{
			CreatedAt:      time.Now().Add(-5 * time.Minute),
			QuestionsAsked: []string{"T01", "T02"},
			FailedRequests: 0,
			Timeouts:       0,
			Unhandled5xx:   0,
		}

		breakdown := service.CalculateScore(session)

		assert.Equal(t, 1000, breakdown.Base)
	})

	t.Run("calculates time bonus correctly", func(t *testing.T) {
		session := &domain.Session{
			CreatedAt:      time.Now().Add(-100 * time.Second),
			QuestionsAsked: []string{"T01"},
			FailedRequests: 0,
		}

		breakdown := service.CalculateScore(session)

		assert.Greater(t, breakdown.TimeBonus, 0)
		assert.LessOrEqual(t, breakdown.TimeBonus, 600)
	})

	t.Run("calculates question bonus correctly", func(t *testing.T) {
		session := &domain.Session{
			CreatedAt:      time.Now(),
			QuestionsAsked: []string{"T01", "T02", "T03"},
			FailedRequests: 0,
		}

		breakdown := service.CalculateScore(session)

		expectedBonus := 300 - (20 * 3)
		assert.Equal(t, expectedBonus, breakdown.QuestionBonus)
	})

	t.Run("calculates reliability penalty correctly", func(t *testing.T) {
		session := &domain.Session{
			CreatedAt:      time.Now(),
			QuestionsAsked: []string{"T01"},
			FailedRequests: 2,
			Timeouts:       1,
			Unhandled5xx:   1,
		}

		breakdown := service.CalculateScore(session)

		expectedPenalty := (5 * 2) + (2 * 1) + (10 * 1)
		assert.Equal(t, expectedPenalty, breakdown.ReliabilityPenalty)
	})

	t.Run("no bonus for slow completion", func(t *testing.T) {
		session := &domain.Session{
			CreatedAt:      time.Now().Add(-15 * time.Minute),
			QuestionsAsked: []string{"T01"},
			FailedRequests: 0,
		}

		breakdown := service.CalculateScore(session)

		assert.Equal(t, 0, breakdown.TimeBonus)
	})

	t.Run("no question bonus for many questions", func(t *testing.T) {
		questions := make([]string, 20)
		for i := range questions {
			questions[i] = "T01"
		}

		session := &domain.Session{
			CreatedAt:      time.Now(),
			QuestionsAsked: questions,
			FailedRequests: 0,
		}

		breakdown := service.CalculateScore(session)

		assert.Equal(t, 0, breakdown.QuestionBonus)
	})
}
