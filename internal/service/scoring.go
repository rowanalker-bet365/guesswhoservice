package service

import (
	"github.com/guesswho/internal/domain"
)

// ScoringService calculates scores for game sessions
type ScoringService interface {
	CalculateScore(session *domain.Session) domain.ScoreBreakdown
}

type scoringService struct{}

// NewScoringService creates a new scoring service
func NewScoringService() ScoringService {
	return &scoringService{}
}

func (s *scoringService) CalculateScore(session *domain.Session) domain.ScoreBreakdown {
	breakdown := domain.ScoreBreakdown{
		Base:               1000, // Base score for correct guess
		TimeBonus:          s.calculateTimeBonus(session),
		QuestionBonus:      s.calculateQuestionBonus(session),
		ReliabilityPenalty: s.calculateReliabilityPenalty(session),
	}

	return breakdown
}

func (s *scoringService) calculateTimeBonus(session *domain.Session) int {
	elapsed := session.GetElapsedTime()
	bonus := 600 - elapsed
	if bonus < 0 {
		bonus = 0
	}
	return bonus
}

func (s *scoringService) calculateQuestionBonus(session *domain.Session) int {
	questionsAsked := session.GetQuestionsAskedCount()
	bonus := 300 - (20 * questionsAsked)
	if bonus < 0 {
		// Additional penalty for excessive questions beyond the bonus threshold
		excessQuestions := questionsAsked - 15
		if excessQuestions > 0 {
			bonus = -(5 * excessQuestions) // -5 per question beyond 15
		} else {
			bonus = 0
		}
	}
	return bonus
}

func (s *scoringService) calculateReliabilityPenalty(session *domain.Session) int {
	penalty := (5 * session.FailedRequests) +
		(2 * session.Timeouts) +
		(10 * session.Unhandled5xx)
	return penalty
}
