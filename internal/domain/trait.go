package domain

// TraitType represents the type of a trait value
type TraitType string

const (
	TraitTypeBoolean TraitType = "boolean"
	TraitTypeEnum    TraitType = "enum"
	TraitTypeNumeric TraitType = "numeric"
)

// TraitTier represents the complexity/difficulty tier of a trait
type TraitTier string

const (
	TierBasic     TraitTier = "basic"
	TierEncrypted TraitTier = "encrypted"
	TierFlaky     TraitTier = "flaky"
)

// TraitDefinition defines a trait that can be asked about
type TraitDefinition struct {
	QuestionID  string    `json:"questionId"`
	TraitKey    string    `json:"traitKey"`
	Question    string    `json:"question"`
	Type        TraitType `json:"type"`
	Values      []string  `json:"values,omitempty"`
	Tier        TraitTier `json:"tier"`
	Cost        int       `json:"cost"`
	IsEncrypted bool      `json:"-"`
	IsFlaky     bool      `json:"-"`
}

// TraitValue represents an actual trait value for a candidate
type TraitValue struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

// TraitAnswer represents an answer to a trait question
type TraitAnswer struct {
	QuestionID string      `json:"questionId"`
	TraitKey   string      `json:"traitKey"`
	Answer     interface{} `json:"answer,omitempty"`
	Encrypted  string      `json:"encrypted,omitempty"`
	Cipher     string      `json:"cipher,omitempty"`
	KeyHintID  string      `json:"keyHintId,omitempty"`
	Status     string      `json:"status,omitempty"`
	RetryAfter int         `json:"retryAfterMs,omitempty"`
}
