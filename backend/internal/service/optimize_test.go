package service

import (
	"testing"
	"time"

	"github.com/teamopt/backend/internal/runinput"
)

// TestProposalsFromResponseUsesSnapshotRole proves that the solver's role_id is advisory
// only: the role profile published to users is taken from the authoritative snapshot
// candidate, exactly like the grade.
func TestProposalsFromResponseUsesSnapshotRole(t *testing.T) {
	weeks := []string{"2026-W10"}
	snap, err := runinput.Build(runinput.Data{
		RunID: "run_t0001", BriefID: "brf_t0001", Preset: "balanced", K: 1, Seed: 7,
		Horizon: weeks, Weights: map[string]float64{"M1": 1},
		Requirements: []runinput.Requirement{{
			RequirementID: "req_a", RoleID: "qa_engineer", Level: 2, Criticality: "must", Headcount: 1,
			Effort: runinput.Effort{MinPersonDays: 1, LikelyPersonDays: 1, MaxPersonDays: 1},
		}},
		Candidates: []runinput.Candidate{{
			EmployeeID: "emp_ab01", Grade: "M", RoleProfileID: "qa_engineer",
			EmploymentType: "internal", CostCoefficient: 1.4,
			Capacity: []runinput.CapacityRow{{
				Week: "2026-W10", WorkingDays: 5, NormHours: 40, FreeHours: 40,
			}},
		}},
		Constraints: runinput.Constraints{MaxUtilizationRatio: 0.95},
		CreatedAt:   time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}

	resp := optimizeResponse{Variants: []optimizeVariant{{
		VariantID: "var_a", Label: "A",
		Members: []optimizeMember{{
			EmployeeID: "emp_ab01", RoleID: "malicious_role", AllocatedHours: 8,
		}},
	}}}
	proposals := proposalsFromResponse(resp, snap)
	if len(proposals) != 1 || len(proposals[0].Members) != 1 {
		t.Fatalf("unexpected proposals: %#v", proposals)
	}
	member := proposals[0].Members[0]
	if member.RoleID != "qa_engineer" {
		t.Fatalf("member role_id = %q, want authoritative snapshot role qa_engineer", member.RoleID)
	}
	if member.Grade != "M" {
		t.Fatalf("member grade = %q, want authoritative snapshot grade M", member.Grade)
	}

	detail := variantDetail(snap, proposals[0], nil)
	if len(detail.Members) != 1 || detail.Members[0].RoleID != "qa_engineer" {
		t.Fatalf("published variant member role = %#v, want qa_engineer", detail.Members)
	}
}
