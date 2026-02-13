package service

import (
	"testing"

	"github.com/guesswho/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestBoardGenerator_GenerateBoard(t *testing.T) {
	traitCatalog := NewTraitCatalogService()
	generator := NewBoardGeneratorService()

	t.Run("generates correct number of candidates", func(t *testing.T) {
		boardSize := 64
		seed := int64(12345)

		candidates, err := generator.GenerateBoard(seed, boardSize, traitCatalog)

		assert.NoError(t, err)
		assert.Equal(t, boardSize, len(candidates))
	})

	t.Run("candidates have unique IDs", func(t *testing.T) {
		boardSize := 64
		seed := int64(12345)

		candidates, err := generator.GenerateBoard(seed, boardSize, traitCatalog)

		assert.NoError(t, err)

		ids := make(map[string]bool)
		for _, candidate := range candidates {
			assert.False(t, ids[candidate.CandidateID], "duplicate candidate ID: %s", candidate.CandidateID)
			ids[candidate.CandidateID] = true
		}
	})

	t.Run("candidates have all traits", func(t *testing.T) {
		boardSize := 10
		seed := int64(12345)

		candidates, err := generator.GenerateBoard(seed, boardSize, traitCatalog)

		assert.NoError(t, err)
		assert.Greater(t, len(candidates[0].Traits), 0)

		// Check that key traits exist
		_, hasHairColor := candidates[0].Traits["hair_color"]
		assert.True(t, hasHairColor)
	})

	t.Run("different seeds produce different boards", func(t *testing.T) {
		candidates1, _ := generator.GenerateBoard(11111, 10, traitCatalog)
		candidates2, _ := generator.GenerateBoard(22222, 10, traitCatalog)

		// At least some traits should be different
		differentCount := 0
		for key, val1 := range candidates1[0].Traits {
			val2, _ := candidates2[0].Traits[key]
			if val1 != val2 {
				differentCount++
			}
		}
		assert.Greater(t, differentCount, 0, "different seeds should produce different trait values")
	})
}

func TestBoardGenerator_SelectTarget(t *testing.T) {
	traitCatalog := NewTraitCatalogService()
	generator := NewBoardGeneratorService()

	t.Run("selects a target from candidates", func(t *testing.T) {
		seed := int64(12345)
		candidates, _ := generator.GenerateBoard(seed, 64, traitCatalog)

		target := generator.SelectTarget(candidates, seed)

		assert.NotNil(t, target)

		// Verify target is in candidates
		found := false
		for _, c := range candidates {
			if c.CandidateID == target.CandidateID {
				found = true
				break
			}
		}
		assert.True(t, found)
	})

	t.Run("returns nil for empty candidates", func(t *testing.T) {
		seed := int64(12345)
		target := generator.SelectTarget([]*domain.Candidate{}, seed)
		assert.Nil(t, target)
	})
}
