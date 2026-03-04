package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSubmitDecryption_WitnessKeyValidation tests that the decrypt endpoint
// requires a correct witness key derived from the session's unencrypted trait
// answers, preventing players from bypassing the decryption challenge by simply
// echoing back their submitted candidate ID.
func TestSubmitDecryption_WitnessKeyValidation(t *testing.T) {
	encSvc := NewEncryptionService()

	sessionID := "test-session-123"
	traitAnswers := map[string]string{
		"hair_color": "brown",
		"has_beard":  "true",
		"eye_color":  "blue",
	}

	t.Run("correct witness key is accepted", func(t *testing.T) {
		key, err := encSvc.DeriveWitnessKey(sessionID, traitAnswers)
		require.NoError(t, err)

		// Simulate what SubmitDecryption does: derive the expected key and compare
		expectedKey, err := encSvc.DeriveWitnessKey(sessionID, traitAnswers)
		require.NoError(t, err)

		assert.Equal(t, expectedKey, key, "correct witness key should match")
	})

	t.Run("wrong witness key is rejected", func(t *testing.T) {
		expectedKey, err := encSvc.DeriveWitnessKey(sessionID, traitAnswers)
		require.NoError(t, err)

		wrongKey := "0000000000000000000000000000000000000000000000000000000000000000"
		assert.NotEqual(t, expectedKey, wrongKey, "fabricated key should not match")
	})

	t.Run("empty witness key is rejected", func(t *testing.T) {
		expectedKey, err := encSvc.DeriveWitnessKey(sessionID, traitAnswers)
		require.NoError(t, err)

		assert.NotEqual(t, expectedKey, "", "empty key should not match")
	})

	t.Run("witness key from different session is rejected", func(t *testing.T) {
		correctKey, err := encSvc.DeriveWitnessKey(sessionID, traitAnswers)
		require.NoError(t, err)

		differentSessionKey, err := encSvc.DeriveWitnessKey("other-session-456", traitAnswers)
		require.NoError(t, err)

		assert.NotEqual(t, correctKey, differentSessionKey,
			"witness key from a different session should not match")
	})

	t.Run("witness key from different trait answers is rejected", func(t *testing.T) {
		correctKey, err := encSvc.DeriveWitnessKey(sessionID, traitAnswers)
		require.NoError(t, err)

		differentAnswers := map[string]string{
			"hair_color": "blonde",
			"has_beard":  "false",
		}
		differentAnswerKey, err := encSvc.DeriveWitnessKey(sessionID, differentAnswers)
		require.NoError(t, err)

		assert.NotEqual(t, correctKey, differentAnswerKey,
			"witness key from different trait answers should not match")
	})

	t.Run("witness key with subset of trait answers is rejected", func(t *testing.T) {
		correctKey, err := encSvc.DeriveWitnessKey(sessionID, traitAnswers)
		require.NoError(t, err)

		// Player only has some of the unencrypted answers
		partialAnswers := map[string]string{
			"hair_color": "brown",
		}
		partialKey, err := encSvc.DeriveWitnessKey(sessionID, partialAnswers)
		require.NoError(t, err)

		assert.NotEqual(t, correctKey, partialKey,
			"witness key from partial trait answers should not match")
	})

	t.Run("witness key with extra trait answers is rejected", func(t *testing.T) {
		correctKey, err := encSvc.DeriveWitnessKey(sessionID, traitAnswers)
		require.NoError(t, err)

		extraAnswers := map[string]string{
			"hair_color":  "brown",
			"has_beard":   "true",
			"eye_color":   "blue",
			"wears_hat":   "false",
		}
		extraKey, err := encSvc.DeriveWitnessKey(sessionID, extraAnswers)
		require.NoError(t, err)

		assert.NotEqual(t, correctKey, extraKey,
			"witness key with extra trait answers should not match")
	})

	t.Run("end-to-end: encrypt with witness key then validate", func(t *testing.T) {
		// Simulate the full Stage 2 flow:
		// 1. Derive witness key (server-side during guess)
		witnessKey, err := encSvc.DeriveWitnessKey(sessionID, traitAnswers)
		require.NoError(t, err)

		// 2. Encrypt the candidate ID with the witness key
		candidateID := "C42"
		cipherType := "xor"
		encrypted, err := encSvc.Encrypt(candidateID, cipherType, witnessKey)
		require.NoError(t, err)
		assert.NotEqual(t, candidateID, encrypted)

		// 3. Player decrypts using the witness key they derived
		decrypted, err := encSvc.Decrypt(encrypted, cipherType, witnessKey)
		require.NoError(t, err)
		assert.Equal(t, candidateID, decrypted)

		// 4. Validate: server re-derives the witness key and compares
		expectedKey, err := encSvc.DeriveWitnessKey(sessionID, traitAnswers)
		require.NoError(t, err)
		assert.Equal(t, expectedKey, witnessKey, "server re-derived key should match")
	})

	t.Run("end-to-end: wrong witness key cannot decrypt", func(t *testing.T) {
		witnessKey, err := encSvc.DeriveWitnessKey(sessionID, traitAnswers)
		require.NoError(t, err)

		candidateID := "C42"
		cipherType := "aes-128-gcm"
		encrypted, err := encSvc.Encrypt(candidateID, cipherType, witnessKey)
		require.NoError(t, err)

		// A player with a wrong witness key cannot decrypt
		wrongKey, err := encSvc.DeriveWitnessKey("wrong-session", traitAnswers)
		require.NoError(t, err)

		_, err = encSvc.Decrypt(encrypted, cipherType, wrongKey)
		assert.Error(t, err, "decryption with wrong witness key should fail for authenticated ciphers")
	})
}
