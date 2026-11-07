//go:build property

package validator

import (
	"fmt"
	"math"
	"testing"
	"testing/quick"

	"github.com/teamopt/backend/internal/domain"
)

// ---------- property 1: G1 holds when no one is assigned during zero-availability weeks ----------

func TestPropertyValidator_G1NoAbsenceViolation(t *testing.T) {
	v := New()

	err := quick.Check(func(seed uint16, numEmp uint8, numWeeks uint8, numAssign uint8) bool {
		input := buildRandomValidInput(int(seed), int(numEmp%5)+1, int(numWeeks%8)+1, int(numAssign%20)+1)
		caps := buildCapacityLookup(input.CapacityMatrix)

		// Ensure every assignment targets a week with AvailableMinutes > 0.
		// Assignments whose employee has no available week at all are dropped.
		assignments := input.Variant.WeeklyAssignments[:0]
		for _, a := range input.Variant.WeeklyAssignments {
			row, ok := caps.get(a.EmployeeID, a.Week)
			if !ok {
				continue
			}
			if row.AvailableMinutes == 0 {
				found := false
				for _, w := range input.CapacityMatrix[a.EmployeeID] {
					if w.AvailableMinutes > 0 {
						a.Week = w.Week
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}
			assignments = append(assignments, a)
		}
		input.Variant.WeeklyAssignments = assignments

		res, err := v.Validate(input)
		if err != nil {
			t.Fatalf("validate error: %v", err)
		}
		return res.Violations.G1 == 0
	}, &quick.Config{MaxCount: 200})

	if err != nil {
		t.Fatal(err)
	}
}

// ---------- property 2: G2 holds when assignments fit inside free capacity ----------

func TestPropertyValidator_G2NoOverload(t *testing.T) {
	v := New()

	err := quick.Check(func(seed uint16, numEmp uint8, numWeeks uint8) bool {
		input := buildRandomValidInput(int(seed), int(numEmp%5)+1, int(numWeeks%8)+1, 0)

		// Generate assignments that never exceed available minutes per employee-week.
		used := make(map[string]map[string]int) // emp -> week -> used minutes
		for empID, weeks := range input.CapacityMatrix {
			if used[empID] == nil {
				used[empID] = make(map[string]int)
			}
			for _, w := range weeks {
				if w.AvailableMinutes <= 0 {
					continue
				}
				// Use at most 90% of available minutes to stay safely under the limit.
				maxHours := float64(w.AvailableMinutes) / 60.0 * 0.9
				if maxHours < 1 {
					continue
				}
				hours := math.Max(1, math.Floor(maxHours))
				input.Variant.WeeklyAssignments = append(input.Variant.WeeklyAssignments, WeeklyAssignment{
					EmployeeID:    empID,
					RequirementID: input.Requirements[0].RequirementID,
					Week:          w.Week,
					Hours:         hours,
				})
				used[empID][w.Week] += int(hours * 60)
			}
		}

		res, err := v.Validate(input)
		if err != nil {
			t.Fatalf("validate error: %v", err)
		}
		return res.Violations.G2 == 0 && res.Violations.MaxOverloadPct == 0
	}, &quick.Config{MaxCount: 200})

	if err != nil {
		t.Fatal(err)
	}
}

// ---------- property 3: G3 holds when every must requirement is covered or listed as deficit ----------

func TestPropertyValidator_G3MustCoveredOrDeficit(t *testing.T) {
	v := New()

	// Every generated variant covers its single must requirement either by
	// assignment or by an explicit deficit. The validator must report G3=0.
	err := quick.Check(func(seed uint16, coverMust bool) bool {
		input := buildRandomValidInput(int(seed), 3, 4, 0)
		req := Requirement{
			RequirementID: fmt.Sprintf("req_%04d", seed),
			CompetencyID:  "go_backend",
			Level:         3,
			Criticality:   "must",
			Effort: RequirementEffort{
				MinPersonDays:    5,
				LikelyPersonDays: 10,
				MaxPersonDays:    20,
			},
		}
		input.Requirements = append(input.Requirements, req)

		if coverMust {
			// Give the first candidate enough hours at sufficient level to cover the must.
			empID := input.Candidates[0].EmployeeID
			for _, w := range input.CapacityMatrix[empID] {
				if w.AvailableMinutes >= 8*60 {
					input.Variant.WeeklyAssignments = append(input.Variant.WeeklyAssignments, WeeklyAssignment{
						EmployeeID:    empID,
						RequirementID: req.RequirementID,
						Week:          w.Week,
						Hours:         40,
					})
				}
			}
		} else {
			// Explicitly list the must requirement as a deficit.
			input.Variant.Deficits = append(input.Variant.Deficits, VariantDeficit{
				RequirementID:     req.RequirementID,
				PersonDays:        req.Effort.LikelyPersonDays,
				RecommendedAction: "hire",
			})
		}

		res, err := v.Validate(input)
		if err != nil {
			t.Fatalf("validate error: %v", err)
		}
		return res.Violations.G3 == 0 && res.Violations.UncoveredMustNotInDeficit == 0
	}, &quick.Config{MaxCount: 200})

	if err != nil {
		t.Fatal(err)
	}
}

// ---------- helpers for property tests ----------

func buildRandomValidInput(seed, numEmp, numWeeks, numAssign int) ValidateInput {
	weeks := make([]string, numWeeks)
	for i := range weeks {
		weeks[i] = fmt.Sprintf("2026-W%02d", (seed+i)%52+1)
	}

	candidates := make([]RunInputCandidate, numEmp)
	matrix := make(map[string][]domain.WeeklyCapacity, numEmp)
	members := make([]VariantMember, 0, numEmp)
	for i := 0; i < numEmp; i++ {
		empID := fmt.Sprintf("emp_prop%04d", seed*100+i)
		candidates[i] = RunInputCandidate{
			EmployeeID:      empID,
			Grade:           "M",
			CostCoefficient: 1.4,
			CompetencyLevels: map[string]int{
				"go_backend": 4,
			},
		}
		members = append(members, VariantMember{
			EmployeeID:     empID,
			RoleID:         "backend_dev",
			Grade:          "M",
			AllocatedHours: 0,
		})

		capWeeks := make([]domain.WeeklyCapacity, numWeeks)
		for j, w := range weeks {
			// Alternate between fully available and partly booked weeks.
			available := 8 * 60
			absent := 0
			if (seed+i+j)%5 == 0 {
				available = 0
				absent = 8 * 60
			}
			capWeeks[j] = domain.WeeklyCapacity{
				EmployeeID:       empID,
				Week:             w,
				AvailableMinutes: available,
				AssignedMinutes:  0,
				AbsentMinutes:    absent,
				NormMinutes:      8 * 60,
			}
		}
		matrix[empID] = capWeeks
	}

	reqID := fmt.Sprintf("req_%04d", seed)
	assignments := make([]WeeklyAssignment, 0, numAssign)
	for i := 0; i < numAssign; i++ {
		empIdx := (seed + i) % numEmp
		weekIdx := (seed + i*3) % numWeeks
		assignments = append(assignments, WeeklyAssignment{
			EmployeeID:    candidates[empIdx].EmployeeID,
			RequirementID: reqID,
			Week:          weeks[weekIdx],
			Hours:         float64((seed+i)%8 + 1),
		})
	}

	return ValidateInput{
		Variant: TeamVariant{
			VariantID:         fmt.Sprintf("var_%04d", seed),
			RunID:             fmt.Sprintf("run_%04d", seed),
			Members:           members,
			WeeklyAssignments: assignments,
			Deficits:          []VariantDeficit{},
		},
		Requirements: []Requirement{
			{
				RequirementID: reqID,
				CompetencyID:  "go_backend",
				Level:         3,
				Criticality:   "must",
				Effort: RequirementEffort{
					MinPersonDays:    5,
					LikelyPersonDays: 10,
					MaxPersonDays:    20,
				},
			},
		},
		Candidates:     candidates,
		Constraints:    RunConstraints{},
		CapacityMatrix: matrix,
		Horizon:        weeks,
	}
}
