package domain

// Role and Competency mirror the contract taxonomy shapes. Arrays are always serialized
// as JSON arrays (never null): a seed row without synonyms still has an empty list.
type Role struct {
	RoleID           string   `json:"role_id"`
	NameEN           string   `json:"name_en"`
	NameRU           string   `json:"name_ru"`
	Family           string   `json:"family"`
	Synonyms         []string `json:"synonyms"`
	CoreCompetencies []string `json:"core_competencies"`
	TypicalGrades    []string `json:"typical_grades"`
}

type Competency struct {
	CompetencyID string   `json:"competency_id"`
	NameEN       string   `json:"name_en"`
	NameRU       string   `json:"name_ru"`
	Category     string   `json:"category"`
	Synonyms     []string `json:"synonyms"`
	Roles        []string `json:"roles"`
	LevelScale   int      `json:"level_scale"`
}

type RolePage struct {
	Items []Role   `json:"items"`
	Meta  PageMeta `json:"meta"`
}

type CompetencyPage struct {
	Items []Competency `json:"items"`
	Meta  PageMeta     `json:"meta"`
}

type RoleFilter struct {
	Q      string
	Family string
	Limit  int
	Offset int
}

type CompetencyFilter struct {
	RoleID   string
	Category string
	Q        string
	Limit    int
	Offset   int
}
