package service

import (
	"fmt"
	"math/rand"

	"github.com/guesswho/internal/domain"
)

// BoardGeneratorService generates candidate boards for sessions
type BoardGeneratorService interface {
	GenerateBoard(seed int64, boardSize int, traitCatalog TraitCatalogService) ([]*domain.Candidate, error)
	SelectTarget(candidates []*domain.Candidate, seed int64) *domain.Candidate
}

type boardGeneratorService struct{}

// NewBoardGeneratorService creates a new board generator service
func NewBoardGeneratorService() BoardGeneratorService {
	return &boardGeneratorService{}
}

func (s *boardGeneratorService) GenerateBoard(seed int64, boardSize int, traitCatalog TraitCatalogService) ([]*domain.Candidate, error) {
	rng := rand.New(rand.NewSource(seed))
	candidates := make([]*domain.Candidate, 0, boardSize)
	traits := traitCatalog.GetAllTraits()

	// Track generated trait combinations to avoid duplicates
	generated := make(map[string]bool)

	for i := 0; i < boardSize; i++ {
		var candidateTraits map[string]interface{}
		var hash string
		maxAttempts := 100
		attempt := 0

		// Try to generate a unique candidate
		for attempt < maxAttempts {
			candidateTraits = s.generateTraits(rng, traits)
			hash = s.hashTraits(candidateTraits)

			if !generated[hash] {
				generated[hash] = true
				break
			}
			attempt++
		}

		if attempt == maxAttempts {
			return nil, fmt.Errorf("failed to generate unique candidate after %d attempts", maxAttempts)
		}

		candidateID := fmt.Sprintf("c_%03d", i+1)
		displayName := fmt.Sprintf("Candidate %03d", i+1)
		candidate := domain.NewCandidate(candidateID, displayName, candidateTraits)
		candidates = append(candidates, candidate)
	}

	return candidates, nil
}

func (s *boardGeneratorService) SelectTarget(candidates []*domain.Candidate, seed int64) *domain.Candidate {
	if len(candidates) == 0 {
		return nil
	}
	rng := rand.New(rand.NewSource(seed + 1000)) // Different seed for target selection
	idx := rng.Intn(len(candidates))
	return candidates[idx]
}

func (s *boardGeneratorService) generateTraits(rng *rand.Rand, traitDefs []domain.TraitDefinition) map[string]interface{} {
	traits := make(map[string]interface{})

	for _, traitDef := range traitDefs {
		var value interface{}

		switch traitDef.Type {
		case domain.TraitTypeBoolean:
			value = rng.Float64() < 0.5

		case domain.TraitTypeEnum:
			if len(traitDef.Values) > 0 {
				// Use weighted distribution to make traits more balanced
				value = traitDef.Values[rng.Intn(len(traitDef.Values))]
			}

		case domain.TraitTypeNumeric:
			// For numeric traits (if any in the future)
			value = rng.Intn(100)
		}

		traits[traitDef.TraitKey] = value
	}

	return traits
}

func (s *boardGeneratorService) hashTraits(traits map[string]interface{}) string {
	// Simple hash function to detect duplicates
	// Uses a subset of key traits to allow some variation
	keyTraits := []string{
		"hair_color", "eye_color", "wears_glasses", "year_group",
		"faculty", "top_color", "primary_style",
	}

	hash := ""
	for _, key := range keyTraits {
		if val, exists := traits[key]; exists {
			hash += fmt.Sprintf("%v|", val)
		}
	}
	return hash
}
