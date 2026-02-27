package service

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
)

// testKey is a 32-byte hex-encoded key used across tests
const testKey = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"

func TestEncryptionService_Encrypt(t *testing.T) {
	svc := NewEncryptionService()

	t.Run("AES-256-GCM encrypts plaintext successfully", func(t *testing.T) {
		plaintext := "JavaScript"

		encrypted, err := svc.Encrypt(plaintext, "AES-256-GCM", testKey)

		assert.NoError(t, err)
		assert.NotEmpty(t, encrypted)
		// Result should be valid hex
		_, hexErr := hex.DecodeString(encrypted)
		assert.NoError(t, hexErr)
	})

	t.Run("AES-256-CBC encrypts plaintext successfully", func(t *testing.T) {
		plaintext := "Python"

		encrypted, err := svc.Encrypt(plaintext, "AES-256-CBC", testKey)

		assert.NoError(t, err)
		assert.NotEmpty(t, encrypted)
	})

	t.Run("XOR encrypts plaintext successfully", func(t *testing.T) {
		plaintext := "TypeScript"

		encrypted, err := svc.Encrypt(plaintext, "XOR", testKey)

		assert.NoError(t, err)
		assert.NotEmpty(t, encrypted)
	})

	t.Run("different plaintexts produce different encrypted values (GCM)", func(t *testing.T) {
		encrypted1, _ := svc.Encrypt("Python", "AES-256-GCM", testKey)
		encrypted2, _ := svc.Encrypt("JavaScript", "AES-256-GCM", testKey)

		assert.NotEqual(t, encrypted1, encrypted2)
	})

	t.Run("unsupported cipher returns error", func(t *testing.T) {
		_, err := svc.Encrypt("test", "UNKNOWN-CIPHER", testKey)
		assert.Error(t, err)
	})

	t.Run("invalid key returns error", func(t *testing.T) {
		_, err := svc.Encrypt("test", "AES-256-GCM", "not-valid-hex")
		assert.Error(t, err)
	})
}

func TestEncryptionService_GetCipherInfo(t *testing.T) {
	svc := NewEncryptionService()

	t.Run("returns cipher info for AES-256-GCM", func(t *testing.T) {
		info := svc.GetCipherInfo("AES-256-GCM", testKey)
		assert.Equal(t, "AES-256-GCM", info.Cipher)
		assert.Equal(t, testKey, info.Key)
		assert.Equal(t, "hex", info.Encoding)
		assert.NotEmpty(t, info.Hint)
	})

	t.Run("returns cipher info for AES-256-CBC", func(t *testing.T) {
		info := svc.GetCipherInfo("AES-256-CBC", testKey)
		assert.Equal(t, "AES-256-CBC", info.Cipher)
		assert.Equal(t, testKey, info.Key)
	})

	t.Run("returns cipher info for XOR", func(t *testing.T) {
		info := svc.GetCipherInfo("XOR", testKey)
		assert.Equal(t, "XOR", info.Cipher)
		assert.Equal(t, testKey, info.Key)
	})
}
