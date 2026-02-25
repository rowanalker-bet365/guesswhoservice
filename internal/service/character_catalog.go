package service

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/guesswho/internal/domain"
)

// CharacterCatalogService manages the catalog of all available characters
type CharacterCatalogService interface {
	GetAllCharacters() []domain.Candidate
	GetCharacterByID(id string) (*domain.Candidate, bool)
}

type characterCatalogService struct {
	characters map[string]domain.Candidate
}

type characterJSON struct {
	ID     string                 `json:"id"`
	Name   string                 `json:"name"`
	Image  string                 `json:"image"`
	Traits map[string]interface{} `json:"traits"`
}

// NewCharacterCatalogService creates a new character catalog service by loading data from a JSON file
func NewCharacterCatalogService(dataPath string) (CharacterCatalogService, error) {
	data, err := os.ReadFile(dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read characters file: %w", err)
	}

	var charsJSON []characterJSON
	if err := json.Unmarshal(data, &charsJSON); err != nil {
		return nil, fmt.Errorf("failed to parse characters JSON: %w", err)
	}

	characters := make(map[string]domain.Candidate)
	for _, c := range charsJSON {
		characters[c.ID] = domain.Candidate{
			CandidateID: c.ID,
			DisplayName: c.Name,
			ImagePath:   c.Image,
			Traits:      c.Traits,
		}
	}

	return &characterCatalogService{
		characters: characters,
	}, nil
}

func (s *characterCatalogService) GetAllCharacters() []domain.Candidate {
	// Ensure deterministic ordering
	keys := make([]string, 0, len(s.characters))
	for k := range s.characters {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]domain.Candidate, 0, len(s.characters))
	for _, k := range keys {
		result = append(result, s.characters[k])
	}
	return result
}

func (s *characterCatalogService) GetCharacterByID(id string) (*domain.Candidate, bool) {
	char, exists := s.characters[id]
	if !exists {
		return nil, false
	}
	return &char, true
}