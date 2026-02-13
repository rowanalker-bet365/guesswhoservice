package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEncryptionService_Encrypt(t *testing.T) {
	service := NewEncryptionService()

	t.Run("encrypts plaintext successfully", func(t *testing.T) {
		plaintext := "JavaScript"

		encrypted, err := service.Encrypt(plaintext)

		assert.NoError(t, err)
		assert.NotEmpty(t, encrypted)
	})

	t.Run("different plaintexts produce different encrypted values", func(t *testing.T) {
		encrypted1, _ := service.Encrypt("Python")
		encrypted2, _ := service.Encrypt("JavaScript")

		assert.NotEqual(t, encrypted1, encrypted2)
	})
}

func TestEncryptionService_Decrypt(t *testing.T) {
	service := NewEncryptionService()

	t.Run("decrypts encrypted value successfully", func(t *testing.T) {
		plaintext := "Python"
		encrypted, _ := service.Encrypt(plaintext)

		decrypted, err := service.Decrypt(encrypted)

		assert.NoError(t, err)
		assert.Equal(t, plaintext, decrypted)
	})

	t.Run("round trip encryption/decryption", func(t *testing.T) {
		original := "TypeScript"

		encrypted, err := service.Encrypt(original)
		assert.NoError(t, err)

		decrypted, err := service.Decrypt(encrypted)
		assert.NoError(t, err)

		assert.Equal(t, original, decrypted)
	})

	t.Run("returns error for invalid payload", func(t *testing.T) {
		_, err := service.Decrypt("invalid base64")
		assert.Error(t, err)
	})
}
