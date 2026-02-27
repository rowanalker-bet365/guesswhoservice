package service

import "fmt"

// ChaosError represents a chaos-injected failure that should be returned
// as a real HTTP error to the client.
type ChaosError struct {
	StatusCode   int
	ErrorCode    string
	Message      string
	RetryAfterMs int
}

func (e *ChaosError) Error() string {
	return fmt.Sprintf("chaos: %d %s", e.StatusCode, e.Message)
}
