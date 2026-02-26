package service

import (
	"fmt"
	"testing"

	"github.com/guesswho/internal/domain"
	"github.com/stretchr/testify/assert"
)

// mockCharacterCatalog is a test double for CharacterCatalogService.
type mockCharacterCatalog struct {
	characters []domain.Candidate
}

func (m *mockCharacterCatalog) GetAllCharacters() []domain.Candidate {
	return m.characters
}

func (m *mockCharacterCatalog) GetCharacterByID(id string) (*domain.Candidate, bool) {
	for _, c := range m.characters {
		if c.CandidateID == id {
			cp := c
			return &cp, true
		}
	}
	return nil, false
}

// newMockCatalog builds a catalog with n characters using real-style IDs (C01–C64)
// and a single trait key T01 so tests remain independent of the real JSON file.
func newMockCatalog(n int) CharacterCatalogService {
	chars := make([]domain.Candidate, n)
	for i := 0; i < n; i++ {
		chars[i] = domain.Candidate{
			CandidateID: fmt.Sprintf("C%02d", i+1),
			DisplayName: "",
			ImagePath:   fmt.Sprintf("/public/images/C%02d.png", i+1),
			Traits:      map[string]interface{}{"T01": true},
		}
	}
	return &mockCharacterCatalog{characters: chars}
}

func TestBoardGenerator_GenerateBoard(t *testing.T) {
	catalog := newMockCatalog(64)
	generator := NewBoardGeneratorService()

	t.Run("board contains all 64 characters", func(t *testing.T) {
		seed := int64(12345)

		candidates, err := generator.GenerateBoard(seed, catalog)

		assert.NoError(t, err)
		assert.Equal(t, 64, len(candidates))
	})

	t.Run("candidates have unique IDs", func(t *testing.T) {
		seed := int64(12345)

		candidates, err := generator.GenerateBoard(seed, catalog)

		assert.NoError(t, err)

		ids := make(map[string]bool)
		for _, candidate := range candidates {
			assert.False(t, ids[candidate.CandidateID], "duplicate candidate ID: %s", candidate.CandidateID)
			ids[candidate.CandidateID] = true
		}
	})

	t.Run("candidates have traits", func(t *testing.T) {
		seed := int64(12345)

		candidates, err := generator.GenerateBoard(seed, catalog)

		assert.NoError(t, err)
		assert.Greater(t, len(candidates[0].Traits), 0)

		// Verify the real-style trait key T01 is present
		_, hasT01 := candidates[0].Traits["T01"]
		assert.True(t, hasT01)
	})

	t.Run("different seeds produce different board orders", func(t *testing.T) {
		candidates1, _ := generator.GenerateBoard(11111, catalog)
		candidates2, _ := generator.GenerateBoard(22222, catalog)

		// At least one position should differ between the two shuffles
		differentCount := 0
		for i := range candidates1 {
			if candidates1[i].CandidateID != candidates2[i].CandidateID {
				differentCount++
			}
		}
		assert.Greater(t, differentCount, 0, "different seeds should produce different board orders")
	})

	t.Run("same seed produces the same board order", func(t *testing.T) {
		candidates1, _ := generator.GenerateBoard(99999, catalog)
		candidates2, _ := generator.GenerateBoard(99999, catalog)

		for i := range candidates1 {
			assert.Equal(t, candidates1[i].CandidateID, candidates2[i].CandidateID,
				"same seed should produce identical board order at position %d", i)
		}
	})
}

func TestBoardGenerator_SelectTarget(t *testing.T) {
	catalog := newMockCatalog(64)
	generator := NewBoardGeneratorService()

	t.Run("selects a target from candidates", func(t *testing.T) {
		seed := int64(12345)
		candidates, _ := generator.GenerateBoard(seed, catalog)

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
