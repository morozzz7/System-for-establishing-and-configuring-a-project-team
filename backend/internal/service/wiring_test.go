package service

import (
	"testing"
	"time"

	"github.com/teamopt/backend/internal/metrics"
	"github.com/teamopt/backend/internal/mlclient"
	"github.com/teamopt/backend/internal/runinput"
)

func wiringSnapshot(t *testing.T) *runinput.Snapshot {
	t.Helper()
	snap, err := runinput.Build(runinput.Data{
		RunID: "run_w0001", BriefID: "brf_w0001", Preset: "balanced", K: 1, Seed: 7,
		Horizon: []string{"2026-W10", "2026-W11", "2026-W12"},
		Weights: map[string]float64{"M1": 1},
		Requirements: []runinput.Requirement{{
			RequirementID: "req_a", RoleID: "qa_engineer", Level: 2, Criticality: "must", Headcount: 1,
			Effort: runinput.Effort{MinPersonDays: 3, LikelyPersonDays: 3, MaxPersonDays: 3},
		}},
		Candidates: []runinput.Candidate{{
			EmployeeID: "emp_ab01", Grade: "M", RoleProfileID: "qa_engineer",
			EmploymentType: "internal", CostCoefficient: 1.4,
			Capacity: []runinput.CapacityRow{
				{Week: "2026-W10", WorkingDays: 5, NormHours: 40, FreeHours: 40},
				{Week: "2026-W11", WorkingDays: 5, NormHours: 40, FreeHours: 40},
				{Week: "2026-W12", WorkingDays: 5, NormHours: 40, FreeHours: 40},
			},
		}},
		Constraints: runinput.Constraints{MaxUtilizationRatio: 0.95},
		CreatedAt:   time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	return snap
}

func TestRunPayloadReportsObservedMLOutcome(t *testing.T) {
	data := runinput.Data{
		Horizon: []string{"2026-W10"}, Preset: "balanced", K: 1, Seed: 7, TimeLimitMS: 10000,
	}
	outcome := mlclient.Outcome{Calls: 2, Status: mlclient.StatusDegraded, Latency: 120 * time.Millisecond}
	payload := runPayload("run_w0001", "brf_w0001", data, nil,
		metrics.VariantSetDiagnostics{}, optimizeResponse{}, 0, time.Now(), outcome)
	diagnostics, ok := payload["diagnostics"].(map[string]any)
	if !ok {
		t.Fatalf("diagnostics shape: %#v", payload["diagnostics"])
	}
	if diagnostics["ml_status"] != mlclient.StatusDegraded || diagnostics["ml_attempts"] != 2 {
		t.Fatalf("diagnostics do not reflect the observed outcome: %#v", diagnostics)
	}
	if diagnostics["solve_time_ms"] != 120 {
		t.Fatalf("solve_time_ms = %v, want client latency 120 ms", diagnostics["solve_time_ms"])
	}
}

func TestContractDeficitsUsesCounterfactualWeeks(t *testing.T) {
	snap := wiringSnapshot(t)
	proposal := metrics.Proposal{
		VariantID: "var_a",
		Members:   []metrics.ProposalMember{{EmployeeID: "emp_ab01"}},
		Assignments: []metrics.ProposalAssignment{{
			EmployeeID: "emp_ab01", RequirementID: "req_a", Week: "2026-W10", Hours: 8,
		}},
	}
	result := metrics.VariantResult{
		VariantID: "var_a",
		Deficits: []metrics.Deficit{{
			RequirementID: "req_a", PersonDays: 1, ExternalCooperationFlag: true,
		}},
	}
	deficits := contractDeficits(snap, proposal, result, scheduleConfig(snap))
	if len(deficits) != 1 {
		t.Fatalf("deficits = %#v", deficits)
	}
	payload, ok := deficits[0].(map[string]any)
	if !ok {
		t.Fatalf("deficit payload shape: %#v", deficits[0])
	}
	weeks, present := payload["weeks"]
	if !present {
		t.Fatalf("free capacity exists, so a duration shift must be computed: %#v", payload)
	}
	shift, ok := weeks.(float64)
	if !ok || shift < 0 || shift > 1 {
		t.Fatalf("weeks = %#v, want a shift in [0, person_days]", weeks)
	}
	if actions, ok := payload["recommended_actions"].([]any); !ok || len(actions) == 0 {
		t.Fatalf("recommended_actions = %#v", payload["recommended_actions"])
	}
}
