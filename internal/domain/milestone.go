package domain

// Milestone is a type-safe identifier for team achievements.
type Milestone string

const (
	MilestoneM1 Milestone = "M1" // First Round Started
	MilestoneM2 Milestone = "M2" // First Successful Question
	MilestoneM3 Milestone = "M3" // Elimination Working
	MilestoneM4 Milestone = "M4" // First Correct Solve
	MilestoneM5 Milestone = "M5" // Encrypted Answer Handled
	MilestoneS1 Milestone = "S1" // Efficiency
	MilestoneS2 Milestone = "S2" // Automation
	MilestoneS3 Milestone = "S3" // Resilience
)

// Milestone score bonuses
const (
	CoreMilestoneScore    = 1000 // Points awarded for M1–M5
	StretchMilestoneScore = 2000 // Points awarded for S1–S3
)

// MilestoneInfo provides human-readable metadata for a milestone.
type MilestoneInfo struct {
	ID          Milestone
	Name        string
	Description string
	Score       int
}

// AllMilestones is the complete ordered list of milestones with their metadata.
var AllMilestones = []MilestoneInfo{
	{MilestoneM1, "First Round Started", "Started your first game session", CoreMilestoneScore},
	{MilestoneM2, "First Successful Question", "Received your first successful answer", CoreMilestoneScore},
	{MilestoneM3, "Elimination Working", "Asked 3+ questions in a single session", CoreMilestoneScore},
	{MilestoneM4, "First Correct Solve", "Correctly identified a character", CoreMilestoneScore},
	{MilestoneM5, "Encrypted Answer Handled", "Successfully decoded an encrypted answer", CoreMilestoneScore},
	{MilestoneS1, "Efficiency", "Solved a character with 10 or fewer questions", StretchMilestoneScore},
	{MilestoneS2, "Automation", "Achieved 3+ total correct solves", StretchMilestoneScore},
	{MilestoneS3, "Resilience", "Successfully received a valid response during an active chaos window", StretchMilestoneScore},
}

// MilestoneScoreMap provides a quick lookup from milestone ID to its score value.
var MilestoneScoreMap = func() map[Milestone]int {
	m := make(map[Milestone]int, len(AllMilestones))
	for _, mi := range AllMilestones {
		m[mi.ID] = mi.Score
	}
	return m
}()
