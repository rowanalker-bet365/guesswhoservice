package domain

// TraitType represents the type of a trait value
type TraitType string

const (
	TraitTypeBoolean TraitType = "boolean"
	TraitTypeEnum    TraitType = "enum"
	TraitTypeNumeric TraitType = "numeric"
)

// TraitDefinition defines a trait that can be asked about
type TraitDefinition struct {
	QuestionID string    `json:"questionId"`
	TraitKey   string    `json:"traitKey"`
	Question   string    `json:"question"`
	Type       TraitType `json:"type"`
	Values     []string  `json:"values,omitempty"`
}

// TraitValue represents an actual trait value for a candidate
type TraitValue struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

// TraitAnswer represents an answer to a trait question.
// The shape of the response depends on whether the answer is blocked, encrypted, or plain.
//
// Blocked:   {"questionId":"..","traitKey":"..","blocked":true,"message":".."}
// Encrypted: {"questionId":"..","traitKey":"..","encrypted":true,"ciphertext":"..","cipherType":".."}
// Plain:     {"questionId":"..","traitKey":"..","encrypted":false,"answer":true}
type TraitAnswer struct {
	QuestionID string `json:"questionId"`
	TraitKey   string `json:"traitKey"`

	// Blocked response fields
	Blocked bool   `json:"blocked,omitempty"`
	Message string `json:"message,omitempty"`

	// Encrypted/plain response fields (nil for blocked responses)
	Encrypted  *bool  `json:"encrypted,omitempty"`
	Ciphertext string `json:"ciphertext,omitempty"`
	CipherType string `json:"cipherType,omitempty"`
	Answer     *bool  `json:"answer,omitempty"`
}
