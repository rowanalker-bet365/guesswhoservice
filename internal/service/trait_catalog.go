package service

import (
	"sort"

	"github.com/guesswho/internal/domain"
)

// TraitCatalogService manages the catalog of all available traits
type TraitCatalogService interface {
	GetAllTraits() []domain.TraitDefinition
	GetTraitByID(questionID string) (*domain.TraitDefinition, bool)
	GetTraitByKey(traitKey string) (*domain.TraitDefinition, bool)
}

type traitCatalogService struct {
	traits map[string]domain.TraitDefinition // keyed by questionID
}

// NewTraitCatalogService creates a new trait catalog service
func NewTraitCatalogService() TraitCatalogService {
	return &traitCatalogService{
		traits: initializeTraits(),
	}
}

func (s *traitCatalogService) GetAllTraits() []domain.TraitDefinition {
	// Ensure deterministic ordering for stable board generation
	keys := make([]string, 0, len(s.traits))
	for k := range s.traits {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]domain.TraitDefinition, 0, len(s.traits))
	for _, k := range keys {
		result = append(result, s.traits[k])
	}
	return result
}

func (s *traitCatalogService) GetTraitByID(questionID string) (*domain.TraitDefinition, bool) {
	trait, exists := s.traits[questionID]
	if !exists {
		return nil, false
	}
	return &trait, true
}

func (s *traitCatalogService) GetTraitByKey(traitKey string) (*domain.TraitDefinition, bool) {
	for _, trait := range s.traits {
		if trait.TraitKey == traitKey {
			return &trait, true
		}
	}
	return nil, false
}

// initializeTraits creates the complete catalog of 64 traits
func initializeTraits() map[string]domain.TraitDefinition {
	traits := make(map[string]domain.TraitDefinition)

	// A) Appearance (12 traits)
	traits["T01"] = domain.TraitDefinition{QuestionID: "T01", TraitKey: "hair_color", Question: "Hair color?", Type: domain.TraitTypeEnum, Values: []string{"black", "brown", "blonde", "red"}}
	traits["T02"] = domain.TraitDefinition{QuestionID: "T02", TraitKey: "hair_length", Question: "Hair length?", Type: domain.TraitTypeEnum, Values: []string{"short", "medium", "long"}}
	traits["T03"] = domain.TraitDefinition{QuestionID: "T03", TraitKey: "eye_color", Question: "Eye color?", Type: domain.TraitTypeEnum, Values: []string{"brown", "blue", "green", "gray"}}
	traits["T04"] = domain.TraitDefinition{QuestionID: "T04", TraitKey: "wears_glasses", Question: "Wearing glasses?", Type: domain.TraitTypeBoolean}
	traits["T05"] = domain.TraitDefinition{QuestionID: "T05", TraitKey: "has_beard", Question: "Has beard?", Type: domain.TraitTypeBoolean}
	traits["T06"] = domain.TraitDefinition{QuestionID: "T06", TraitKey: "has_moustache", Question: "Has moustache?", Type: domain.TraitTypeBoolean}
	traits["T07"] = domain.TraitDefinition{QuestionID: "T07", TraitKey: "has_freckles", Question: "Has freckles?", Type: domain.TraitTypeBoolean}
	traits["T08"] = domain.TraitDefinition{QuestionID: "T08", TraitKey: "has_dimples", Question: "Has dimples?", Type: domain.TraitTypeBoolean}
	traits["T09"] = domain.TraitDefinition{QuestionID: "T09", TraitKey: "skin_tone", Question: "Skin tone category?", Type: domain.TraitTypeEnum, Values: []string{"A", "B", "C", "D"}}
	traits["T10"] = domain.TraitDefinition{QuestionID: "T10", TraitKey: "face_shape", Question: "Face shape?", Type: domain.TraitTypeEnum, Values: []string{"oval", "round", "square", "heart"}}
	traits["T11"] = domain.TraitDefinition{QuestionID: "T11", TraitKey: "eyebrow_style", Question: "Eyebrow style?", Type: domain.TraitTypeEnum, Values: []string{"straight", "arched", "thick", "thin"}}
	traits["T12"] = domain.TraitDefinition{QuestionID: "T12", TraitKey: "smile", Question: "Smiling in photo?", Type: domain.TraitTypeBoolean}

	// B) Clothing (10 traits)
	traits["T13"] = domain.TraitDefinition{QuestionID: "T13", TraitKey: "top_color", Question: "Top color?", Type: domain.TraitTypeEnum, Values: []string{"black", "white", "blue", "green", "red", "gray"}}
	traits["T14"] = domain.TraitDefinition{QuestionID: "T14", TraitKey: "wears_hoodie", Question: "Wearing hoodie?", Type: domain.TraitTypeBoolean}
	traits["T15"] = domain.TraitDefinition{QuestionID: "T15", TraitKey: "wears_jacket", Question: "Wearing jacket?", Type: domain.TraitTypeBoolean}
	traits["T16"] = domain.TraitDefinition{QuestionID: "T16", TraitKey: "wears_shirt_collar", Question: "Collar visible?", Type: domain.TraitTypeBoolean}
	traits["T17"] = domain.TraitDefinition{QuestionID: "T17", TraitKey: "bottom_type", Question: "Bottom type?", Type: domain.TraitTypeEnum, Values: []string{"jeans", "chinos", "shorts", "skirt"}}
	traits["T18"] = domain.TraitDefinition{QuestionID: "T18", TraitKey: "shoe_type", Question: "Shoe type?", Type: domain.TraitTypeEnum, Values: []string{"trainers", "boots", "formal", "sandals"}}
	traits["T19"] = domain.TraitDefinition{QuestionID: "T19", TraitKey: "hat_type", Question: "Wearing a hat?", Type: domain.TraitTypeEnum, Values: []string{"none", "cap", "beanie"}}
	traits["T20"] = domain.TraitDefinition{QuestionID: "T20", TraitKey: "pattern", Question: "Clothing pattern?", Type: domain.TraitTypeEnum, Values: []string{"plain", "striped", "checked"}}
	traits["T21"] = domain.TraitDefinition{QuestionID: "T21", TraitKey: "primary_style", Question: "Style?", Type: domain.TraitTypeEnum, Values: []string{"casual", "sporty", "smart", "alt"}}
	traits["T22"] = domain.TraitDefinition{QuestionID: "T22", TraitKey: "wears_uniform", Question: "Wearing uniform?", Type: domain.TraitTypeBoolean}

	// C) Accessories (8 traits)
	traits["T23"] = domain.TraitDefinition{QuestionID: "T23", TraitKey: "wears_watch", Question: "Wearing watch?", Type: domain.TraitTypeBoolean}
	traits["T24"] = domain.TraitDefinition{QuestionID: "T24", TraitKey: "wears_ring", Question: "Wearing ring?", Type: domain.TraitTypeBoolean}
	traits["T25"] = domain.TraitDefinition{QuestionID: "T25", TraitKey: "wears_necklace", Question: "Wearing necklace?", Type: domain.TraitTypeBoolean}
	traits["T26"] = domain.TraitDefinition{QuestionID: "T26", TraitKey: "wears_earrings", Question: "Wearing earrings?", Type: domain.TraitTypeBoolean}
	traits["T27"] = domain.TraitDefinition{QuestionID: "T27", TraitKey: "carries_backpack", Question: "Has backpack?", Type: domain.TraitTypeBoolean}
	traits["T28"] = domain.TraitDefinition{QuestionID: "T28", TraitKey: "carries_laptop", Question: "Has laptop?", Type: domain.TraitTypeBoolean}
	traits["T29"] = domain.TraitDefinition{QuestionID: "T29", TraitKey: "phone_os", Question: "Phone OS?", Type: domain.TraitTypeEnum, Values: []string{"iOS", "Android", "other"}}
	traits["T30"] = domain.TraitDefinition{QuestionID: "T30", TraitKey: "headphone_type", Question: "Headphones?", Type: domain.TraitTypeEnum, Values: []string{"none", "in_ear", "over_ear"}}

	// D) University / Academic (12 traits)
	traits["T31"] = domain.TraitDefinition{QuestionID: "T31", TraitKey: "year_group", Question: "Year group?", Type: domain.TraitTypeEnum, Values: []string{"1", "2", "3", "4"}}
	traits["T32"] = domain.TraitDefinition{QuestionID: "T32", TraitKey: "faculty", Question: "Faculty?", Type: domain.TraitTypeEnum, Values: []string{"Eng", "Sci", "Biz", "Arts"}}
	traits["T33"] = domain.TraitDefinition{QuestionID: "T33", TraitKey: "timetable_morning", Question: "Has morning class today?", Type: domain.TraitTypeBoolean}
	traits["T34"] = domain.TraitDefinition{QuestionID: "T34", TraitKey: "lab_user", Question: "Has a lab module?", Type: domain.TraitTypeBoolean}
	traits["T35"] = domain.TraitDefinition{QuestionID: "T35", TraitKey: "group_project", Question: "In a group project?", Type: domain.TraitTypeBoolean}
	traits["T36"] = domain.TraitDefinition{QuestionID: "T36", TraitKey: "preferred_editor", Question: "Preferred editor?", Type: domain.TraitTypeEnum, Values: []string{"VSCode", "IntelliJ", "Vim", "Other"}}
	traits["T37"] = domain.TraitDefinition{QuestionID: "T37", TraitKey: "os_primary", Question: "Primary OS?", Type: domain.TraitTypeEnum, Values: []string{"Windows", "macOS", "Linux"}}
	traits["T38"] = domain.TraitDefinition{QuestionID: "T38", TraitKey: "attends_society", Question: "In a society?", Type: domain.TraitTypeBoolean}
	traits["T39"] = domain.TraitDefinition{QuestionID: "T39", TraitKey: "commute_type", Question: "Commute type?", Type: domain.TraitTypeEnum, Values: []string{"walk", "bike", "bus", "train", "car"}}
	traits["T40"] = domain.TraitDefinition{QuestionID: "T40", TraitKey: "library_frequency", Question: "Library use?", Type: domain.TraitTypeEnum, Values: []string{"low", "medium", "high"}}
	traits["T41"] = domain.TraitDefinition{QuestionID: "T41", TraitKey: "caffeine", Question: "Drinks caffeine?", Type: domain.TraitTypeBoolean}
	traits["T42"] = domain.TraitDefinition{QuestionID: "T42", TraitKey: "part_time_job", Question: "Has part-time job?", Type: domain.TraitTypeBoolean}

	// E) Tech Preferences / Habits (8 traits)
	traits["T43"] = domain.TraitDefinition{QuestionID: "T43", TraitKey: "favorite_language", Question: "Favorite language?", Type: domain.TraitTypeEnum, Values: []string{"Python", "JS", "Java", "C#", "Other"}}
	traits["T44"] = domain.TraitDefinition{QuestionID: "T44", TraitKey: "git_user", Question: "Uses git daily?", Type: domain.TraitTypeBoolean}
	traits["T45"] = domain.TraitDefinition{QuestionID: "T45", TraitKey: "cloud_interest", Question: "Interested in cloud?", Type: domain.TraitTypeBoolean}
	traits["T46"] = domain.TraitDefinition{QuestionID: "T46", TraitKey: "ai_interest", Question: "Interested in AI?", Type: domain.TraitTypeBoolean}
	traits["T47"] = domain.TraitDefinition{QuestionID: "T47", TraitKey: "gaming", Question: "Plays games weekly?", Type: domain.TraitTypeBoolean}
	traits["T48"] = domain.TraitDefinition{QuestionID: "T48", TraitKey: "keyboard_layout", Question: "Keyboard layout?", Type: domain.TraitTypeEnum, Values: []string{"QWERTY", "AZERTY", "Other"}}
	traits["T49"] = domain.TraitDefinition{QuestionID: "T49", TraitKey: "two_factor", Question: "Uses 2FA?", Type: domain.TraitTypeBoolean}
	traits["T50"] = domain.TraitDefinition{QuestionID: "T50", TraitKey: "password_manager", Question: "Uses password manager?", Type: domain.TraitTypeBoolean}

	// F) Lifestyle / Preferences (10 traits)
	traits["T51"] = domain.TraitDefinition{QuestionID: "T51", TraitKey: "sport", Question: "Plays a sport?", Type: domain.TraitTypeBoolean}
	traits["T52"] = domain.TraitDefinition{QuestionID: "T52", TraitKey: "music_genre", Question: "Music genre?", Type: domain.TraitTypeEnum, Values: []string{"pop", "rock", "hiphop", "jazz", "edm", "other"}}
	traits["T53"] = domain.TraitDefinition{QuestionID: "T53", TraitKey: "food_pref", Question: "Food preference?", Type: domain.TraitTypeEnum, Values: []string{"veg", "nonveg", "vegan", "other"}}
	traits["T54"] = domain.TraitDefinition{QuestionID: "T54", TraitKey: "sleep_pattern", Question: "Sleep pattern?", Type: domain.TraitTypeEnum, Values: []string{"early", "normal", "nightowl"}}
	traits["T55"] = domain.TraitDefinition{QuestionID: "T55", TraitKey: "pet_person", Question: "Likes pets?", Type: domain.TraitTypeBoolean}
	traits["T56"] = domain.TraitDefinition{QuestionID: "T56", TraitKey: "coffee_order", Question: "Coffee order?", Type: domain.TraitTypeEnum, Values: []string{"latte", "americano", "tea", "none"}}
	traits["T57"] = domain.TraitDefinition{QuestionID: "T57", TraitKey: "weekend_style", Question: "Weekend style?", Type: domain.TraitTypeEnum, Values: []string{"chill", "study", "outdoors", "work"}}
	traits["T58"] = domain.TraitDefinition{QuestionID: "T58", TraitKey: "travel", Question: "Traveled this year?", Type: domain.TraitTypeBoolean}
	traits["T59"] = domain.TraitDefinition{QuestionID: "T59", TraitKey: "reading", Question: "Reads books monthly?", Type: domain.TraitTypeBoolean}
	traits["T60"] = domain.TraitDefinition{QuestionID: "T60", TraitKey: "social_media", Question: "Uses social daily?", Type: domain.TraitTypeBoolean}

	// G) Security / Verification (4 traits)
	traits["T61"] = domain.TraitDefinition{QuestionID: "T61", TraitKey: "training_provider", Question: "Training provider?", Type: domain.TraitTypeEnum, Values: []string{"A", "B", "C", "D"}}
	traits["T62"] = domain.TraitDefinition{QuestionID: "T62", TraitKey: "id_verified", Question: "Identity verified?", Type: domain.TraitTypeBoolean}
	traits["T63"] = domain.TraitDefinition{QuestionID: "T63", TraitKey: "eligibility", Question: "Eligible for withdrawal?", Type: domain.TraitTypeBoolean}
	traits["T64"] = domain.TraitDefinition{QuestionID: "T64", TraitKey: "risk_band", Question: "Risk band?", Type: domain.TraitTypeEnum, Values: []string{"low", "medium", "high"}}

	return traits
}
