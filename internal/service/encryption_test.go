package service

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testKey is an 8-byte hex-encoded key used for caesar, vigenere, and xor tests.
const testKey = "0102030405060708"

// testKey16 is a 16-byte hex-encoded key for aes-128-gcm tests.
const testKey16 = "00112233445566778899aabbccddeeff"

// testKey32 is a 32-byte hex-encoded key for aes-256-cbc tests.
const testKey32 = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

func TestEncryptionService_Encrypt(t *testing.T) {
	svc := NewEncryptionService()

	t.Run("caesar shifts letters", func(t *testing.T) {
		encrypted, err := svc.Encrypt("abc", "caesar", testKey)
		assert.NoError(t, err)
		assert.NotEmpty(t, encrypted)
		assert.NotEqual(t, "abc", encrypted)
		assert.Len(t, encrypted, 3)
	})

	t.Run("vigenere encrypts plaintext", func(t *testing.T) {
		encrypted, err := svc.Encrypt("Hello", "vigenere", testKey)
		assert.NoError(t, err)
		assert.NotEmpty(t, encrypted)
		assert.NotEqual(t, "Hello", encrypted)
	})

	t.Run("xor encrypts plaintext to hex", func(t *testing.T) {
		encrypted, err := svc.Encrypt("Hello", "xor", testKey)
		assert.NoError(t, err)
		assert.NotEmpty(t, encrypted)
		_, hexErr := hex.DecodeString(encrypted)
		assert.NoError(t, hexErr)
	})

	t.Run("aes-128-gcm encrypts plaintext to hex", func(t *testing.T) {
		encrypted, err := svc.Encrypt("Hello", "aes-128-gcm", testKey16)
		assert.NoError(t, err)
		assert.NotEmpty(t, encrypted)
		data, hexErr := hex.DecodeString(encrypted)
		assert.NoError(t, hexErr)
		// nonce (12) + ciphertext (5) + tag (16) = 33 bytes minimum
		assert.GreaterOrEqual(t, len(data), 12+16)
	})

	t.Run("aes-128-gcm produces different ciphertext each call (random nonce)", func(t *testing.T) {
		enc1, err1 := svc.Encrypt("Hello", "aes-128-gcm", testKey16)
		enc2, err2 := svc.Encrypt("Hello", "aes-128-gcm", testKey16)
		assert.NoError(t, err1)
		assert.NoError(t, err2)
		assert.NotEqual(t, enc1, enc2)
	})

	t.Run("aes-256-cbc encrypts plaintext to hex", func(t *testing.T) {
		encrypted, err := svc.Encrypt("Hello", "aes-256-cbc", testKey32)
		assert.NoError(t, err)
		assert.NotEmpty(t, encrypted)
		data, hexErr := hex.DecodeString(encrypted)
		assert.NoError(t, hexErr)
		// iv (16) + at least one block (16) = 32 bytes minimum
		assert.GreaterOrEqual(t, len(data), 32)
	})

	t.Run("aes-256-cbc produces different ciphertext each call (random IV)", func(t *testing.T) {
		enc1, err1 := svc.Encrypt("Hello", "aes-256-cbc", testKey32)
		enc2, err2 := svc.Encrypt("Hello", "aes-256-cbc", testKey32)
		assert.NoError(t, err1)
		assert.NoError(t, err2)
		assert.NotEqual(t, enc1, enc2)
	})

	t.Run("unsupported cipher returns error", func(t *testing.T) {
		_, err := svc.Encrypt("test", "UNKNOWN-CIPHER", testKey)
		assert.Error(t, err)
	})

	t.Run("xor with invalid key returns error", func(t *testing.T) {
		_, err := svc.Encrypt("test", "xor", "not-valid-hex")
		assert.Error(t, err)
	})

	t.Run("aes-128-gcm with short key returns error", func(t *testing.T) {
		_, err := svc.Encrypt("test", "aes-128-gcm", "0011")
		assert.Error(t, err)
	})

	t.Run("aes-256-cbc with wrong key length returns error", func(t *testing.T) {
		_, err := svc.Encrypt("test", "aes-256-cbc", testKey16)
		assert.Error(t, err)
	})
}

func TestEncryptionService_Decrypt(t *testing.T) {
	svc := NewEncryptionService()

	ciphers := []struct {
		name      string
		plaintext string
		keyHex    string
	}{
		{"caesar", "Hello, World!", testKey},
		{"xor", "Hello, World!", testKey},
		{"vigenere", "Hello, World!", testKey},
		{"aes-128-gcm", "Hello, World!", testKey16},
		{"aes-256-cbc", "Hello, World!", testKey32},
	}

	for _, tc := range ciphers {
		tc := tc
		t.Run(tc.name+" round-trip", func(t *testing.T) {
			encrypted, err := svc.Encrypt(tc.plaintext, tc.name, tc.keyHex)
			require.NoError(t, err)

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

	t.Run("xor with invalid key returns error", func(t *testing.T) {
		_, err := svc.Decrypt("48656c6c6f", "xor", "not-valid-hex")
		assert.Error(t, err)
	})

	t.Run("aes-128-gcm with tampered ciphertext returns error", func(t *testing.T) {
		encrypted, err := svc.Encrypt("Hello", "aes-128-gcm", testKey16)
		require.NoError(t, err)
		// Flip last hex char to corrupt the GCM tag
		tampered := encrypted[:len(encrypted)-1] + "0"
		if tampered == encrypted {
			tampered = encrypted[:len(encrypted)-1] + "f"
		}
		_, err = svc.Decrypt(tampered, "aes-128-gcm", testKey16)
		assert.Error(t, err)
	})

	t.Run("aes-256-cbc with invalid hex returns error", func(t *testing.T) {
		_, err := svc.Decrypt("not-valid-hex!!!", "aes-256-cbc", testKey32)
		assert.Error(t, err)
	})
}

func TestEncryptionService_GetCipherInfo(t *testing.T) {
	svc := NewEncryptionService()

	t.Run("caesar returns numeric shift as key", func(t *testing.T) {
		info, err := svc.GetCipherInfo("caesar", testKey)
		assert.NoError(t, err)
		assert.Equal(t, "caesar", info.Cipher)
		assert.NotEmpty(t, info.Key)
		assert.Equal(t, "plaintext", info.Encoding)
		assert.NotEmpty(t, info.Hint)
	})

	t.Run("vigenere returns keyword as key", func(t *testing.T) {
		info, err := svc.GetCipherInfo("vigenere", testKey)
		assert.NoError(t, err)
		assert.Equal(t, "vigenere", info.Cipher)
		assert.NotEmpty(t, info.Key)
		assert.True(t, strings.ToLower(info.Key) == info.Key, "keyword should be lowercase")
		assert.Equal(t, "plaintext", info.Encoding)
	})

	t.Run("xor returns full key hex", func(t *testing.T) {
		info, err := svc.GetCipherInfo("xor", testKey)
		assert.NoError(t, err)
		assert.Equal(t, "xor", info.Cipher)
		assert.Equal(t, testKey, info.Key)
		assert.Equal(t, "hex", info.Encoding)
	})

	t.Run("aes-128-gcm returns first 16 bytes as key", func(t *testing.T) {
		info, err := svc.GetCipherInfo("aes-128-gcm", testKey16)
		assert.NoError(t, err)
		assert.Equal(t, "aes-128-gcm", info.Cipher)
		assert.Equal(t, testKey16[:32], info.Key)
		assert.Equal(t, "hex", info.Encoding)
		assert.NotEmpty(t, info.Hint)
	})

	t.Run("aes-256-cbc returns full key hex", func(t *testing.T) {
		info, err := svc.GetCipherInfo("aes-256-cbc", testKey32)
		assert.NoError(t, err)
		assert.Equal(t, "aes-256-cbc", info.Cipher)
		assert.Equal(t, testKey32, info.Key)
		assert.Equal(t, "hex", info.Encoding)
		assert.NotEmpty(t, info.Hint)
	})

	t.Run("unsupported cipher returns error", func(t *testing.T) {
		_, err := svc.GetCipherInfo("base64", testKey)
		assert.Error(t, err)
	})
}

func TestEncryptionService_DeriveWitnessKey(t *testing.T) {
	svc := NewEncryptionService()

	t.Run("returns 64-char hex string", func(t *testing.T) {
		key, err := svc.DeriveWitnessKey("session-abc", map[string]string{
			"hair_color": "brown",
			"has_beard":  "true",
		})
		assert.NoError(t, err)
		assert.Len(t, key, 64)
		_, hexErr := hex.DecodeString(key)
		assert.NoError(t, hexErr)
	})

	t.Run("same inputs produce same key (deterministic)", func(t *testing.T) {
		answers := map[string]string{"hair_color": "brown", "has_beard": "true"}
		key1, err1 := svc.DeriveWitnessKey("session-abc", answers)
		key2, err2 := svc.DeriveWitnessKey("session-abc", answers)
		assert.NoError(t, err1)
		assert.NoError(t, err2)
		assert.Equal(t, key1, key2)
	})

	t.Run("different sessionID produces different key", func(t *testing.T) {
		answers := map[string]string{"hair_color": "brown"}
		key1, _ := svc.DeriveWitnessKey("session-aaa", answers)
		key2, _ := svc.DeriveWitnessKey("session-bbb", answers)
		assert.NotEqual(t, key1, key2)
	})

	t.Run("different answers produce different key", func(t *testing.T) {
		key1, _ := svc.DeriveWitnessKey("session-abc", map[string]string{"hair_color": "brown"})
		key2, _ := svc.DeriveWitnessKey("session-abc", map[string]string{"hair_color": "blonde"})
		assert.NotEqual(t, key1, key2)
	})

	t.Run("map ordering does not affect result", func(t *testing.T) {
		answers1 := map[string]string{"a": "1", "b": "2", "c": "3"}
		answers2 := map[string]string{"c": "3", "a": "1", "b": "2"}
		key1, err1 := svc.DeriveWitnessKey("session-abc", answers1)
		key2, err2 := svc.DeriveWitnessKey("session-abc", answers2)
		assert.NoError(t, err1)
		assert.NoError(t, err2)
		assert.Equal(t, key1, key2)
	})

	t.Run("empty answers map produces a key", func(t *testing.T) {
		key, err := svc.DeriveWitnessKey("session-abc", map[string]string{})
		assert.NoError(t, err)
		assert.Len(t, key, 64)
	})
}
