// Package validator independently verifies team variants against hard constraints G1-G5.
// Types in this file are local to the validator and mirror the relevant parts of
// contracts/openapi.yaml without introducing infrastructure dependencies.
package validator

import "github.com/teamopt/backend/internal/domain"

// TeamVariant is a complete proposed team roster with weekly assignments and deficits.
type TeamVariant struct {
	VariantID         string
	RunID             string
	Members           []VariantMember
	WeeklyAssignments []WeeklyAssignment
	Deficits          []VariantDeficit
	ReasonCodes       []string
}

// VariantMember is one employee on the team.
type VariantMember struct {
	EmployeeID      string
	RoleID          string
	Grade           string
	AllocatedHours  float64
	AssignmentShare float64
	CompetenceFit   float64
}

// WeeklyAssignment is the atomic unit of work assigned to an employee in a week.
type WeeklyAssignment struct {
	EmployeeID    string
	RequirementID string
	Week          string // ISO week key, e.g. "2026-W05"
	Hours         float64
	Share         float64
}

// VariantDeficit lists a requirement that cannot be covered internally.
type VariantDeficit struct {
	RequirementID     string
	PersonDays        float64
	RecommendedAction string
}

// Requirement is the validator-local view of a brief requirement.
type Requirement struct {
	RequirementID string
	CompetencyID  string
	Level         int
	Criticality   string // must, should, nice
	Effort        RequirementEffort
}

// RequirementEffort is a three-point effort estimate in person-days.
type RequirementEffort struct {
	MinPersonDays    float64
	LikelyPersonDays float64
	MaxPersonDays    float64
}

// RunInputCandidate carries employee-level data needed for validation.
// It extends the openapi RunInputCandidate with competency levels, which the
// optimizer request omits but the validator needs for G3 coverage checks.
type RunInputCandidate struct {
	EmployeeID       string
	Grade            string
	CostCoefficient  float64
	CompetencyLevels map[string]int
}

// RunConstraints is the validator-local view of G4 user constraints.
type RunConstraints struct {
	ForceInclude   []string
	ForceExclude   []string
	MaxTeamSize    int
	MinTeamSize    int
	BudgetCapUnits float64
	FreezeMembers  []string
}

// ValidateInput is the authoritative snapshot a variant is validated against.
// CapacityMatrix uses the same shape returned by service.CapacityService.GetCapacityMatrix:
// map[employeeID][]WeeklyCapacity.
type ValidateInput struct {
	Variant        TeamVariant
	Requirements   []Requirement
	Candidates     []RunInputCandidate
	Constraints    RunConstraints
	CapacityMatrix map[string][]domain.WeeklyCapacity
	Horizon        []string
}

// ValidationResult is the outcome of validating a variant.
type ValidationResult struct {
	GatePass    bool
	Violations  GateViolations
	ReasonCodes []string
	Details     []string
}
