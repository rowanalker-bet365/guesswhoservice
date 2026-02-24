package domain

// Candidate represents a person on the game board
type Candidate struct {
	CandidateID string                 `json:"candidateId"`
	DisplayName string                 `json:"displayName"`
	ImagePath   string                 `json:"imagePath"`
	Traits      map[string]interface{} `json:"traits"`
}

// NewCandidate creates a new candidate with the given traits
func NewCandidate(id string, displayName string, imagePath string, traits map[string]interface{}) *Candidate {
	return &Candidate{
		CandidateID: id,
		DisplayName: displayName,
		ImagePath:   imagePath,
		Traits:      traits,
	}
}

// GetTrait returns the value of a specific trait
func (c *Candidate) GetTrait(key string) (interface{}, bool) {
	val, exists := c.Traits[key]
	return val, exists
}

// HasTrait checks if the candidate has a specific trait value
func (c *Candidate) HasTrait(key string, value interface{}) bool {
	val, exists := c.Traits[key]
	if !exists {
		return false
	}
	return val == value
}
