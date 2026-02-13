package service

import (
	"encoding/base64"
	"fmt"
)

// EncryptionService handles encryption and decryption of trait answers
// Simplified per Solution spec: returns "b64:<base64>" and decodes from it.
type EncryptionService interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(encrypted string) (string, error)
}

type encryptionService struct{}

// NewEncryptionService creates a new encryption service
func NewEncryptionService() EncryptionService {
	return &encryptionService{}
}

func (s *encryptionService) Encrypt(plaintext string) (string, error) {
	// Simple base64 encoding with a payload prefix
	payload := fmt.Sprintf("PAYLOAD::%s", plaintext)
	encoded := base64.StdEncoding.EncodeToString([]byte(payload))
	return fmt.Sprintf("b64:%s", encoded), nil
}

func (s *encryptionService) Decrypt(encrypted string) (string, error) {
	// Remove the "b64:" prefix if present
	if len(encrypted) > 4 && encrypted[:4] == "b64:" {
		encrypted = encrypted[4:]
	}

	decoded, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", fmt.Errorf("failed to decode: %w", err)
	}

	payload := string(decoded)

	// Remove the "PAYLOAD::" prefix
	if len(payload) > 9 && payload[:9] == "PAYLOAD::" {
		return payload[9:], nil
	}

	return payload, nil
}
