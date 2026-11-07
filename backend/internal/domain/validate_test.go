package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teamopt/backend/internal/domain"
)

func TestGenerateID(t *testing.T) {
	t.Run("employee prefix", func(t *testing.T) {
		id := domain.GenerateID("emp_", 6)
		assert.Regexp(t, `^emp_[a-z0-9]{6}$`, id)
	})
	t.Run("project prefix", func(t *testing.T) {
		id := domain.GenerateID("prj_", 8)
		assert.Regexp(t, `^prj_[a-z0-9]{8}$`, id)
	})
	t.Run("uniqueness", func(t *testing.T) {
		ids := make(map[string]bool, 100)
		for i := 0; i < 100; i++ {
			id := domain.GenerateID("emp_", 8)
			assert.False(t, ids[id], "duplicate ID generated: %s", id)
			ids[id] = true
		}
	})
}

func TestValidateEmployeeCreate(t *testing.T) {
	tests := []struct {
		name      string
		input     domain.EmployeeCreate
		wantErr   bool
		errFields []string
	}{
		{
			name: "valid input",
			input: domain.EmployeeCreate{
				EmployeeID:     "emp_abcd1234",
				Grade:          "S",
				RoleProfileID:  "backend_engineer",
				UnitID:         "unt_core",
				EmploymentType: "internal",
			},
			wantErr: false,
		},
		{
			name: "invalid employee_id pattern",
			input: domain.EmployeeCreate{
				EmployeeID:     "invalid",
				Grade:          "S",
				RoleProfileID:  "backend_engineer",
				UnitID:         "unt_core",
				EmploymentType: "internal",
			},
			wantErr:   true,
			errFields: []string{"employee_id"},
		},
		{
			name: "invalid grade",
			input: domain.EmployeeCreate{
				EmployeeID:     "emp_abcd1234",
				Grade:          "X",
				RoleProfileID:  "backend_engineer",
				UnitID:         "unt_core",
				EmploymentType: "internal",
			},
			wantErr:   true,
			errFields: []string{"grade"},
		},
		{
			name: "invalid employment type",
			input: domain.EmployeeCreate{
				EmployeeID:     "emp_abcd1234",
				Grade:          "S",
				RoleProfileID:  "backend_engineer",
				UnitID:         "unt_core",
				EmploymentType: "freelance",
			},
			wantErr:   true,
			errFields: []string{"employment_type"},
		},
		{
			name: "invalid unit_id",
			input: domain.EmployeeCreate{
				EmployeeID:     "emp_abcd1234",
				Grade:          "S",
				RoleProfileID:  "backend_engineer",
				UnitID:         "invalid",
				EmploymentType: "internal",
			},
			wantErr:   true,
			errFields: []string{"unit_id"},
		},
		{
			name: "invalid capacity profile",
			input: domain.EmployeeCreate{
				EmployeeID:     "emp_abcd1234",
				Grade:          "S",
				RoleProfileID:  "backend_engineer",
				UnitID:         "unt_core",
				EmploymentType: "internal",
				CapacityProfile: &domain.CapacityProfile{
					WeeklyHoursNorm:    0,
					ReserveRatio:       0.6,
					MaxUtilizationRatio: 1.5,
				},
			},
			wantErr:   true,
			errFields: []string{"capacity_profile.weekly_hours_norm", "capacity_profile.reserve_ratio", "capacity_profile.max_utilization_ratio"},
		},
		{
			name: "invalid cost coefficient",
			input: domain.EmployeeCreate{
				EmployeeID:      "emp_abcd1234",
				Grade:           "S",
				RoleProfileID:   "backend_engineer",
				UnitID:          "unt_core",
				EmploymentType:  "internal",
				CostCoefficient: float64Ptr(10.0),
			},
			wantErr:   true,
			errFields: []string{"cost_coefficient"},
		},
		{
			name: "multiple errors",
			input: domain.EmployeeCreate{
				EmployeeID:     "bad",
				Grade:          "Z",
				RoleProfileID:  "backend_engineer",
				UnitID:         "unt_core",
				EmploymentType: "internal",
			},
			wantErr:   true,
			errFields: []string{"employee_id", "grade"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.ValidateEmployeeCreate(tc.input, "req_test123456")
			if tc.wantErr {
				require.NotNil(t, p, "expected validation error")
				assert.Equal(t, 422, p.Status)
				for _, field := range tc.errFields {
					found := false
					for _, e := range p.Errors {
						if e.Field == field {
							found = true
							break
						}
					}
					assert.True(t, found, "expected error for field %s, got errors: %+v", field, p.Errors)
				}
			} else {
				assert.Nil(t, p)
			}
		})
	}
}

func TestValidateProjectCreate(t *testing.T) {
	tests := []struct {
		name    string
		input   domain.ProjectCreate
		wantErr bool
	}{
		{
			name: "valid",
			input: domain.ProjectCreate{
				Code:           "CRM-2026",
				Name:           "CRM rework",
				Status:         "active",
				Priority:       2,
				Criticality:    "high",
				StartWeek:      "2025-W40",
				EndWeekPlanned: "2026-W20",
			},
			wantErr: false,
		},
		{
			name: "invalid status",
			input: domain.ProjectCreate{
				Code:           "CRM-2026",
				Name:           "CRM rework",
				Status:         "invalid",
				Priority:       2,
				Criticality:    "high",
				StartWeek:      "2025-W40",
				EndWeekPlanned: "2026-W20",
			},
			wantErr: true,
		},
		{
			name: "invalid priority",
			input: domain.ProjectCreate{
				Code:           "CRM-2026",
				Name:           "CRM rework",
				Status:         "active",
				Priority:       7,
				Criticality:    "high",
				StartWeek:      "2025-W40",
				EndWeekPlanned: "2026-W20",
			},
			wantErr: true,
		},
		{
			name: "invalid week format",
			input: domain.ProjectCreate{
				Code:           "CRM-2026",
				Name:           "CRM rework",
				Status:         "active",
				Priority:       2,
				Criticality:    "high",
				StartWeek:      "2026-01",
				EndWeekPlanned: "2026-W20",
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.ValidateProjectCreate(tc.input, "req_test123456")
			if tc.wantErr {
				require.NotNil(t, p)
				assert.Equal(t, 422, p.Status)
			} else {
				assert.Nil(t, p)
			}
		})
	}
}

func TestValidateAbsenceCreate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		start, _ := domain.ParseDate("2026-02-16")
		end, _ := domain.ParseDate("2026-02-18")
		input := domain.AbsenceCreate{
			EmployeeID:  "emp_abcd1234",
			Type:        "vacation",
			StartDate:   start,
			EndDate:     end,
			HoursPerDay: 8,
		}
		p := domain.ValidateAbsenceCreate(input, "req_test123456")
		assert.Nil(t, p)
	})

	t.Run("end before start", func(t *testing.T) {
		start, _ := domain.ParseDate("2026-02-18")
		end, _ := domain.ParseDate("2026-02-16")
		input := domain.AbsenceCreate{
			EmployeeID:  "emp_abcd1234",
			Type:        "vacation",
			StartDate:   start,
			EndDate:     end,
			HoursPerDay: 8,
		}
		p := domain.ValidateAbsenceCreate(input, "req_test123456")
		require.NotNil(t, p)
		assert.Equal(t, 422, p.Status)
	})

	t.Run("invalid type", func(t *testing.T) {
		start, _ := domain.ParseDate("2026-02-16")
		end, _ := domain.ParseDate("2026-02-18")
		input := domain.AbsenceCreate{
			EmployeeID:  "emp_abcd1234",
			Type:        "holiday",
			StartDate:   start,
			EndDate:     end,
			HoursPerDay: 8,
		}
		p := domain.ValidateAbsenceCreate(input, "req_test123456")
		require.NotNil(t, p)
	})

	t.Run("hours_per_day out of range", func(t *testing.T) {
		start, _ := domain.ParseDate("2026-02-16")
		end, _ := domain.ParseDate("2026-02-18")
		input := domain.AbsenceCreate{
			EmployeeID:  "emp_abcd1234",
			Type:        "vacation",
			StartDate:   start,
			EndDate:     end,
			HoursPerDay: 15,
		}
		p := domain.ValidateAbsenceCreate(input, "req_test123456")
		require.NotNil(t, p)
	})
}

func TestValidateAssignmentCreate(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		fe := domain.ValidateAssignmentCreate(domain.AssignmentCreate{
			EmployeeID: "emp_abcd1234",
			RoleID:     "backend_engineer",
			Week:       "2026-W03",
			Hours:      20,
		}, 0)
		assert.Empty(t, fe.Code)
	})

	t.Run("hours zero", func(t *testing.T) {
		fe := domain.ValidateAssignmentCreate(domain.AssignmentCreate{
			EmployeeID: "emp_abcd1234",
			RoleID:     "backend_engineer",
			Week:       "2026-W03",
			Hours:      0,
		}, 0)
		assert.Equal(t, "VALUE_OUT_OF_RANGE", fe.Code)
	})

	t.Run("hours too high", func(t *testing.T) {
		fe := domain.ValidateAssignmentCreate(domain.AssignmentCreate{
			EmployeeID: "emp_abcd1234",
			RoleID:     "backend_engineer",
			Week:       "2026-W03",
			Hours:      61,
		}, 0)
		assert.Equal(t, "VALUE_OUT_OF_RANGE", fe.Code)
	})

	t.Run("invalid week", func(t *testing.T) {
		fe := domain.ValidateAssignmentCreate(domain.AssignmentCreate{
			EmployeeID: "emp_abcd1234",
			RoleID:     "backend_engineer",
			Week:       "2026-03",
			Hours:      20,
		}, 0)
		assert.Equal(t, "PATTERN_MISMATCH", fe.Code)
	})
}

func TestValidateWeekKey(t *testing.T) {
	assert.True(t, domain.ValidateWeekKey("2026-W01"))
	assert.True(t, domain.ValidateWeekKey("2026-W53"))
	assert.True(t, domain.ValidateWeekKey("2025-W40"))
	assert.False(t, domain.ValidateWeekKey("2026-W00"))
	assert.False(t, domain.ValidateWeekKey("2026-W54"))
	assert.False(t, domain.ValidateWeekKey("2026-01"))
	assert.False(t, domain.ValidateWeekKey(""))
}

func float64Ptr(v float64) *float64 { return &v }
