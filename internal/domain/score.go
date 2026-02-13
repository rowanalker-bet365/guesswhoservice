package domain

// ScoreBreakdown represents the detailed scoring breakdown
type ScoreBreakdown struct {
	Base               int `json:"base"`
	TimeBonus          int `json:"timeBonus"`
	QuestionBonus      int `json:"questionBonus"`
	ReliabilityPenalty int `json:"reliabilityPenalty"`
}

// GuessResult represents the result of a guess submission
type GuessResult struct {
	Correct   bool           `json:"correct"`
	Score     int            `json:"score"`
	Breakdown ScoreBreakdown `json:"breakdown"`
	Stats     SessionStats   `json:"stats"`
}

// SessionStats represents statistics about a session
type SessionStats struct {
	TimeSeconds    int `json:"timeSeconds"`
	QuestionsAsked int `json:"questionsAsked"`
	FailedRequests int `json:"failedRequests"`
}

// LeaderboardEntry represents a team's performance
type LeaderboardEntry struct {
	TeamID         string  `json:"teamId"`
	Solves         int     `json:"solves"`
	AvgTimeSeconds float64 `json:"avgTimeSeconds"`
	AvgQuestions   float64 `json:"avgQuestions"`
	TotalScore     int     `json:"totalScore"`
	SuccessRate    float64 `json:"successRate"`
	BestStreak     int     `json:"bestStreak"`
}

// RevealResult represents the result of a reveal operation
type RevealResult struct {
	Target       *Candidate  `json:"target"`
	Stats        SessionStats `json:"stats"`
	SessionEnded bool         `json:"sessionEnded"`
}
