package validator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestG3ExpandsCompetencyDeficitWithoutRequirementIDs(t *testing.T) {
	input := validValidatorInput()
	input.Variant.WeeklyAssignments = nil
	input.Variant.Members = nil
	competency := "go_backend"
	input.Variant.Deficits = []VariantDeficit{{CompetencyID: &competency, PersonDays: 20}}

	res, err := New().Validate(input)
	require.NoError(t, err)
	assert.Equal(t, 0, res.Violations.G3, "a competency deficit must cover every requirement in that competency")
}

func TestG3ExpandsRoleDeficitWithoutRequirementIDs(t *testing.T) {
	input := validValidatorInput()
	input.Variant.WeeklyAssignments = nil
	input.Variant.Members = nil
	input.Requirements[0].CompetencyID = ""
	input.Requirements[0].RoleID = "backend_dev"
	input.Variant.Deficits = []VariantDeficit{{RoleID: "backend_dev", PersonDays: 20}}

	res, err := New().Validate(input)
	require.NoError(t, err)
	assert.Equal(t, 0, res.Violations.G3, "a role deficit must cover every requirement of that role")
}

func TestG3DoesNotExpandUnrelatedDeficit(t *testing.T) {
	input := validValidatorInput()
	input.Variant.WeeklyAssignments = nil
	input.Variant.Members = nil
	role := "qa_engineer"
	input.Variant.Deficits = []VariantDeficit{{RoleID: role, PersonDays: 20}}

	res, err := New().Validate(input)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Violations.G3, "a deficit for a different role must not hide a must requirement")
}

func TestG3ExplicitRequirementIDsDeclaredTogether(t *testing.T) {
	input := validValidatorInput()
	input.Variant.WeeklyAssignments = nil
	input.Variant.Members = nil
	competency := "unrelated_competency"
	input.Variant.Deficits = []VariantDeficit{{
		RequirementIDs: []string{"req_t101"}, CompetencyID: &competency, PersonDays: 20,
	}}

	res, err := New().Validate(input)
	require.NoError(t, err)
	assert.Equal(t, 0, res.Violations.G3, "explicit ids must declare the requirement even when the competency differs")
}
