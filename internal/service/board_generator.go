package service

import (
	"math/rand"

	"github.com/guesswho/internal/domain"
)

// BoardGeneratorService generates candidate boards for sessions
type BoardGeneratorService interface {
	GenerateBoard(seed int64, characterCatalog CharacterCatalogService) ([]*domain.Candidate, error)
	SelectTarget(candidates []*domain.Candidate, seed int64) *domain.Candidate
}

type boardGeneratorService struct{}

// NewBoardGeneratorService creates a new board generator service
func NewBoardGeneratorService() BoardGeneratorService {
	return &boardGeneratorService{}
}

// GenerateBoard loads all characters from the catalog, shuffles them using the
// provided seed, and returns the full shuffled list as the session board.
func (s *boardGeneratorService) GenerateBoard(seed int64, characterCatalog CharacterCatalogService) ([]*domain.Candidate, error) {
	all := characterCatalog.GetAllCharacters()

	// Copy into a pointer slice so each session gets independent copies.
	candidates := make([]*domain.Candidate, len(all))
	for i := range all {
		c := all[i]
		candidates[i] = &c
	}

	// Shuffle using a seeded RNG for reproducibility.
	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})

	return candidates, nil
}

// SelectTarget picks a deterministic target from the candidate list using the given seed.
func (s *boardGeneratorService) SelectTarget(candidates []*domain.Candidate, seed int64) *domain.Candidate {
	if len(candidates) == 0 {
		return nil
	}
	rng := rand.New(rand.NewSource(seed + 1000)) // offset to differ from board shuffle seed
	idx := rng.Intn(len(candidates))
	return candidates[idx]
}
