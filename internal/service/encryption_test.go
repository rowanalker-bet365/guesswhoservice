package service

import (
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
)

// testKey is an 8-byte hex-encoded key used across tests
const testKey = "0102030405060708"

func TestEncryptionService_Encrypt(t *testing.T) {
	svc := NewEncryptionService()

	t.Run("base64 encrypts plaintext successfully", func(t *testing.T) {
		encrypted, err := svc.Encrypt("Python", "base64", testKey)
		assert.NoError(t, err)
		assert.NotEmpty(t, encrypted)
		// Should be valid base64
		decoded, err := base64.StdEncoding.DecodeString(encrypted)
		assert.NoError(t, err)
		assert.Equal(t, "Python", string(decoded))
	})

	t.Run("hex encrypts plaintext successfully", func(t *testing.T) {
		encrypted, err := svc.Encrypt("Python", "hex", testKey)
		assert.NoError(t, err)
		assert.NotEmpty(t, encrypted)
		// Should be valid hex that decodes back to plaintext
		decoded, err := hex.DecodeString(encrypted)
		assert.NoError(t, err)
		assert.Equal(t, "Python", string(decoded))
	})

	t.Run("reverse reverses the string", func(t *testing.T) {
		encrypted, err := svc.Encrypt("Python", "reverse", testKey)
		assert.NoError(t, err)
		assert.Equal(t, "nohtyP", encrypted)
	})

	t.Run("caesar shifts letters", func(t *testing.T) {
		encrypted, err := svc.Encrypt("abc", "caesar", testKey)
		assert.NoError(t, err)
		assert.NotEmpty(t, encrypted)
		// Should not equal plaintext (shift > 0)
		assert.NotEqual(t, "abc", encrypted)
		// Should be same length
		assert.Len(t, encrypted, 3)
	})

	t.Run("xor encrypts plaintext successfully", func(t *testing.T) {
		encrypted, err := svc.Encrypt("Hello", "xor", testKey)
		assert.NoError(t, err)
		assert.NotEmpty(t, encrypted)
		// Should be valid hex
		_, hexErr := hex.DecodeString(encrypted)
		assert.NoError(t, hexErr)
	})

	t.Run("different plaintexts produce different encrypted values", func(t *testing.T) {
		encrypted1, _ := svc.Encrypt("Python", "base64", testKey)
		encrypted2, _ := svc.Encrypt("JavaScript", "base64", testKey)
		assert.NotEqual(t, encrypted1, encrypted2)
	})

	t.Run("unsupported cipher returns error", func(t *testing.T) {
		_, err := svc.Encrypt("test", "UNKNOWN-CIPHER", testKey)
		assert.Error(t, err)
	})

	t.Run("xor with invalid key returns error", func(t *testing.T) {
		_, err := svc.Encrypt("test", "xor", "not-valid-hex")
		assert.Error(t, err)
	})
}

func TestEncryptionService_GetCipherInfo(t *testing.T) {
	svc := NewEncryptionService()

	t.Run("base64 returns no key", func(t *testing.T) {
		info := svc.GetCipherInfo("base64", testKey)
		assert.Equal(t, "base64", info.Cipher)
		assert.Empty(t, info.Key) // no key needed
		assert.Equal(t, "base64", info.Encoding)
		assert.NotEmpty(t, info.Hint)
	})

	t.Run("hex returns no key", func(t *testing.T) {
		info := svc.GetCipherInfo("hex", testKey)
		assert.Equal(t, "hex", info.Cipher)
		assert.Empty(t, info.Key)
		assert.Equal(t, "hex", info.Encoding)
	})

	t.Run("reverse returns no key", func(t *testing.T) {
		info := svc.GetCipherInfo("reverse", testKey)
		assert.Equal(t, "reverse", info.Cipher)
		assert.Empty(t, info.Key)
		assert.Equal(t, "text", info.Encoding)
	})

	t.Run("caesar returns shift as key", func(t *testing.T) {
		info := svc.GetCipherInfo("caesar", testKey)
		assert.Equal(t, "caesar", info.Cipher)
		assert.NotEmpty(t, info.Key) // shift value
		assert.Equal(t, "text", info.Encoding)
	})

	t.Run("xor returns hex key", func(t *testing.T) {
		info := svc.GetCipherInfo("xor", testKey)
		assert.Equal(t, "xor", info.Cipher)
		assert.Equal(t, testKey, info.Key)
		assert.Equal(t, "hex", info.Encoding)
	})
}

func TestEncryptionService_Decrypt(t *testing.T) {
	svc := NewEncryptionService()

	ciphers := []struct {
		name      string
		plaintext string
		keyHex    string
	}{
		{"base64", "Hello, World!", ""},
		{"hex", "Hello, World!", ""},
		{"reverse", "Hello, World!", ""},
		{"caesar", "Hello, World!", testKey},
		{"xor", "Hello, World!", testKey},
		{"vigenere", "Hello, World!", testKey},
		{"xor-base64", "Hello, World!", testKey},
	}

	for _, tc := range ciphers {
		t.Run(tc.name+" round-trip", func(t *testing.T) {
			encrypted, err := svc.Encrypt(tc.plaintext, tc.name, tc.keyHex)
			assert.NoError(t, err)

			decrypted, err := svc.Decrypt(encrypted, tc.name, tc.keyHex)
			assert.NoError(t, err)
			assert.Equal(t, tc.plaintext, decrypted)
		})
	}

	t.Run("unsupported cipher returns error", func(t *testing.T) {
		_, err := svc.Decrypt("test", "UNKNOWN-CIPHER", "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported cipher")
	})

	t.Run("invalid base64 returns error", func(t *testing.T) {
		_, err := svc.Decrypt("not-valid-base64!!!", "base64", "")
		assert.Error(t, err)
	})

	t.Run("invalid hex returns error", func(t *testing.T) {
		_, err := svc.Decrypt("not-valid-hex!!!", "hex", "")
		assert.Error(t, err)
	})

	t.Run("xor with invalid key returns error", func(t *testing.T) {
		_, err := svc.Decrypt("48656c6c6f", "xor", "not-valid-hex")
		assert.Error(t, err)
	})
}
