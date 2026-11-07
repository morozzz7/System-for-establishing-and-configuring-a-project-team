package validator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teamopt/backend/internal/domain"
)

// ---------- fixtures ----------

func validValidatorInput() ValidateInput {
	return ValidateInput{
		Variant: TeamVariant{
			VariantID: "var_t1",
			RunID:     "run_t1",
			Members: []VariantMember{
				{EmployeeID: "emp_alice001", RoleID: "backend_dev", Grade: "M", AllocatedHours: 80},
				{EmployeeID: "emp_bob0001", RoleID: "backend_dev", Grade: "S", AllocatedHours: 80},
			},
			WeeklyAssignments: []WeeklyAssignment{
				{EmployeeID: "emp_alice001", RequirementID: "req_t101", Week: "2026-W05", Hours: 40},
				{EmployeeID: "emp_alice001", RequirementID: "req_t101", Week: "2026-W06", Hours: 40},
				{EmployeeID: "emp_bob0001", RequirementID: "req_t101", Week: "2026-W05", Hours: 40},
				{EmployeeID: "emp_bob0001", RequirementID: "req_t101", Week: "2026-W06", Hours: 40},
			},
			Deficits: []VariantDeficit{},
		},
		Requirements: []Requirement{
			{
				RequirementID: "req_t101",
				CompetencyID:  "go_backend",
				Level:         3,
				Criticality:   "must",
				Effort: RequirementEffort{
					MinPersonDays:    10,
					LikelyPersonDays: 20,
					MaxPersonDays:    30,
				},
			},
		},
		Candidates: []RunInputCandidate{
			{
				EmployeeID:      "emp_alice001",
				Grade:           "M",
				CostCoefficient: 1.4,
				CompetencyLevels: map[string]int{
					"go_backend": 4,
				},
			},
			{
				EmployeeID:      "emp_bob0001",
				Grade:           "S",
				CostCoefficient: 1.9,
				CompetencyLevels: map[string]int{
					"go_backend": 5,
				},
			},
		},
		Constraints: RunConstraints{},
		CapacityMatrix: map[string][]domain.WeeklyCapacity{
			"emp_alice001": {
				{EmployeeID: "emp_alice001", Week: "2026-W05", AvailableMinutes: 40 * 60, AssignedMinutes: 0, AbsentMinutes: 0, NormMinutes: 40 * 60},
				{EmployeeID: "emp_alice001", Week: "2026-W06", AvailableMinutes: 40 * 60, AssignedMinutes: 0, AbsentMinutes: 0, NormMinutes: 40 * 60},
			},
			"emp_bob0001": {
				{EmployeeID: "emp_bob0001", Week: "2026-W05", AvailableMinutes: 40 * 60, AssignedMinutes: 0, AbsentMinutes: 0, NormMinutes: 40 * 60},
				{EmployeeID: "emp_bob0001", Week: "2026-W06", AvailableMinutes: 40 * 60, AssignedMinutes: 0, AbsentMinutes: 0, NormMinutes: 40 * 60},
			},
		},
		Horizon: []string{"2026-W05", "2026-W06"},
	}
}

// ---------- happy path ----------

func TestValidator_AllGatesPass(t *testing.T) {
	v := New()
	input := validValidatorInput()

	res, err := v.Validate(input)
	require.NoError(t, err)
	assert.True(t, res.GatePass)
	assert.Equal(t, 0, res.Violations.G1)
	assert.Equal(t, 0, res.Violations.G2)
	assert.Equal(t, 0, res.Violations.G3)
	assert.Equal(t, 0, res.Violations.G4)
	assert.Equal(t, 0, res.Violations.G5)
	assert.Equal(t, 0, res.Violations.UncoveredMustNotInDeficit)
	assert.Equal(t, 0.0, res.Violations.MaxOverloadPct)
}

// ---------- G1 seeded tests ----------

func TestValidator_G1UnavailableAssigned(t *testing.T) {
	v := New()
	input := validValidatorInput()

	// Make alice unavailable in 2026-W05 (AvailableMinutes == 0).
	input.CapacityMatrix["emp_alice001"][0].AvailableMinutes = 0
	input.CapacityMatrix["emp_alice001"][0].AbsentMinutes = 8 * 60

	res, err := v.Validate(input)
	require.NoError(t, err)
	assert.False(t, res.GatePass)
	assert.Greater(t, res.Violations.G1, 0)
	assert.Contains(t, res.ReasonCodes, reasonG1Unavailable)
	assert.Equal(t, 0, res.Violations.G2) // G2 should not double-count unavailable week.
}

// ---------- G2 seeded tests ----------

func TestValidator_G2Overload(t *testing.T) {
	v := New()
	input := validValidatorInput()

	// Reduce alice's available capacity in 2026-W05 to 8 hours, then assign 12 hours.
	input.CapacityMatrix["emp_alice001"][0].AvailableMinutes = 8 * 60
	input.Variant.WeeklyAssignments[0].Hours = 12

	res, err := v.Validate(input)
	require.NoError(t, err)
	assert.False(t, res.GatePass)
	assert.Greater(t, res.Violations.G2, 0)
	assert.Contains(t, res.ReasonCodes, reasonG2Overload)
	assert.Greater(t, res.Violations.MaxOverloadPct, 0.0)
}

// ---------- G3 seeded tests ----------

func TestValidator_G3MustUncovered(t *testing.T) {
	v := New()
	input := validValidatorInput()

	// Drop bob's assignments so the must requirement is no longer covered.
	input.Variant.WeeklyAssignments = input.Variant.WeeklyAssignments[:2]
	input.Variant.Members = input.Variant.Members[:1]

	res, err := v.Validate(input)
	require.NoError(t, err)
	assert.False(t, res.GatePass)
	assert.Greater(t, res.Violations.G3, 0)
	assert.Greater(t, res.Violations.UncoveredMustNotInDeficit, 0)
	assert.Contains(t, res.ReasonCodes, reasonG3MustUncovered)
}

func TestValidator_G3MustCoveredByDeficit(t *testing.T) {
	v := New()
	input := validValidatorInput()

	// Remove coverage but list the requirement as a deficit -> G3 should be 0.
	input.Variant.WeeklyAssignments = nil
	input.Variant.Members = nil
	input.Variant.Deficits = []VariantDeficit{
		{RequirementID: "req_t101", PersonDays: 20, RecommendedAction: "hire"},
	}

	res, err := v.Validate(input)
	require.NoError(t, err)
	assert.Equal(t, 0, res.Violations.G3)
	assert.Equal(t, 0, res.Violations.UncoveredMustNotInDeficit)
}

// ---------- G4 seeded tests ----------

func TestValidator_G4ForceIncludeViolated(t *testing.T) {
	v := New()
	input := validValidatorInput()
	input.Constraints.ForceInclude = []string{"emp_charlie1"}

	res, err := v.Validate(input)
	require.NoError(t, err)
	assert.False(t, res.GatePass)
	assert.Greater(t, res.Violations.G4, 0)
	assert.Contains(t, res.ReasonCodes, reasonG4ForceInclude)
}

func TestValidator_G4FreezeMemberViolated(t *testing.T) {
	v := New()
	input := validValidatorInput()
	input.Constraints.FreezeMembers = []string{"emp_charlie1"}

	res, err := v.Validate(input)
	require.NoError(t, err)
	assert.False(t, res.GatePass)
	assert.Greater(t, res.Violations.G4, 0)
	assert.Contains(t, res.ReasonCodes, reasonG4ForceInclude)
}

func TestValidator_G4ForceExcludeViolated(t *testing.T) {
	v := New()
	input := validValidatorInput()
	input.Constraints.ForceExclude = []string{"emp_alice001"}

	res, err := v.Validate(input)
	require.NoError(t, err)
	assert.False(t, res.GatePass)
	assert.Greater(t, res.Violations.G4, 0)
	assert.Contains(t, res.ReasonCodes, reasonG4ForceExclude)
}

func TestValidator_G4TeamSizeExceeded(t *testing.T) {
	v := New()
	input := validValidatorInput()
	input.Constraints.MaxTeamSize = 1

	res, err := v.Validate(input)
	require.NoError(t, err)
	assert.False(t, res.GatePass)
	assert.Greater(t, res.Violations.G4, 0)
	assert.Contains(t, res.ReasonCodes, reasonG4TeamSizeExceeded)
}

func TestValidator_G4BudgetExceeded(t *testing.T) {
	v := New()
	input := validValidatorInput()
	// 160 total hours * ~1.4-1.9 cost -> set a tiny budget.
	input.Constraints.BudgetCapUnits = 10

	res, err := v.Validate(input)
	require.NoError(t, err)
	assert.False(t, res.GatePass)
	assert.Greater(t, res.Violations.G4, 0)
	assert.Contains(t, res.ReasonCodes, reasonG4BudgetExceeded)
}

// ---------- G5 seeded tests ----------

func TestValidator_G5UnknownEmployee(t *testing.T) {
	v := New()
	input := validValidatorInput()
	input.Variant.Members[0].EmployeeID = "emp_unknown1"

	res, err := v.Validate(input)
	require.NoError(t, err)
	assert.False(t, res.GatePass)
	assert.Greater(t, res.Violations.G5, 0)
	assert.Contains(t, res.ReasonCodes, reasonG5UnknownEmployee)
}

func TestValidator_G5InvalidWeek(t *testing.T) {
	v := New()
	input := validValidatorInput()
	input.Variant.WeeklyAssignments[0].Week = "2026-W99"

	res, err := v.Validate(input)
	require.NoError(t, err)
	assert.False(t, res.GatePass)
	assert.Greater(t, res.Violations.G5, 0)
	assert.Contains(t, res.ReasonCodes, reasonG5InvalidWeek)
}

func TestValidator_G5WeekOutsideHorizon(t *testing.T) {
	v := New()
	input := validValidatorInput()
	input.Variant.WeeklyAssignments[0].Week = "2026-W07"

	res, err := v.Validate(input)
	require.NoError(t, err)
	assert.False(t, res.GatePass)
	assert.Greater(t, res.Violations.G5, 0)
	assert.Contains(t, res.ReasonCodes, reasonG5InvalidWeek)
}

func TestValidator_G5InvalidRole(t *testing.T) {
	v := New()
	input := validValidatorInput()
	input.Variant.Members[0].RoleID = "BAD ROLE!!!"

	res, err := v.Validate(input)
	require.NoError(t, err)
	assert.False(t, res.GatePass)
	assert.Greater(t, res.Violations.G5, 0)
	assert.Contains(t, res.ReasonCodes, reasonG5InvalidRole)
}

func TestValidator_G5UnknownRequirement(t *testing.T) {
	v := New()
	input := validValidatorInput()
	input.Variant.WeeklyAssignments[0].RequirementID = "req_unknown"

	res, err := v.Validate(input)
	require.NoError(t, err)
	assert.False(t, res.GatePass)
	assert.Greater(t, res.Violations.G5, 0)
	assert.Contains(t, res.ReasonCodes, reasonG5UnknownRequirement)
}

func TestValidator_G1IgnoresZeroHourAssignments(t *testing.T) {
	v := New()
	input := validValidatorInput()
	input.CapacityMatrix["emp_alice001"][0].AvailableMinutes = 0
	input.Variant.WeeklyAssignments[0].Hours = 0

	res, err := v.Validate(input)
	require.NoError(t, err)
	// The zero-hour assignment must not be counted as a G1 violation.
	assert.Equal(t, 0, res.Violations.G1)
}

func TestValidator_G3MissingCompetency(t *testing.T) {
	v := New()
	input := validValidatorInput()
	// Remove bob's competency so the must requirement is no longer covered.
	input.Candidates[1].CompetencyLevels = map[string]int{}

	res, err := v.Validate(input)
	require.NoError(t, err)
	assert.False(t, res.GatePass)
	assert.Greater(t, res.Violations.G3, 0)
	assert.Contains(t, res.ReasonCodes, reasonG3MustUncovered)
}

func TestValidator_G4BudgetUsesGradeFallback(t *testing.T) {
	v := New()
	input := validValidatorInput()
	// Drop cost coefficients so variantCost falls back to grade-based costs.
	input.Candidates[0].CostCoefficient = 0
	input.Candidates[1].CostCoefficient = 0
	input.Constraints.BudgetCapUnits = 10

	res, err := v.Validate(input)
	require.NoError(t, err)
	assert.False(t, res.GatePass)
	assert.Greater(t, res.Violations.G4, 0)
	assert.Contains(t, res.ReasonCodes, reasonG4BudgetExceeded)
}

func TestValidator_G4MinTeamSize(t *testing.T) {
	v := New()
	input := validValidatorInput()
	input.Constraints.MinTeamSize = 5

	res, err := v.Validate(input)
	require.NoError(t, err)
	assert.False(t, res.GatePass)
	assert.Greater(t, res.Violations.G4, 0)
	assert.Contains(t, res.ReasonCodes, reasonG4TeamSizeExceeded)
}

// ---------- determinism ----------

func TestValidator_Determinism(t *testing.T) {
	v := New()
	input := validValidatorInput()

	res1, err := v.Validate(input)
	require.NoError(t, err)
	res2, err := v.Validate(input)
	require.NoError(t, err)

	assert.Equal(t, res1.GatePass, res2.GatePass)
	assert.Equal(t, res1.Violations, res2.Violations)
	assert.Equal(t, res1.ReasonCodes, res2.ReasonCodes)
	assert.Equal(t, res1.Details, res2.Details)
}
