package service

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/teamopt/backend/internal/metrics"
	"github.com/teamopt/backend/internal/runinput"
)

type deficitFixture struct {
	freeHours      float64
	projects       []string
	cost           float64
	reqRole        string
	candRole       string
	deadline       string
	forbidExternal bool
}

func deficitSnapshot(t *testing.T, f deficitFixture) *runinput.Snapshot {
	t.Helper()
	reqRole := f.reqRole
	if reqRole == "" {
		reqRole = "qa_engineer"
	}
	candRole := f.candRole
	if candRole == "" {
		candRole = "qa_engineer"
	}
	cost := f.cost
	if cost == 0 {
		cost = 1.4
	}
	snap, err := runinput.Build(runinput.Data{
		RunID: "run_t0001", BriefID: "brf_t0001", Preset: "balanced", K: 1, Seed: 42,
		Horizon: []string{"2026-W10"}, Weights: map[string]float64{"M1": 1},
		Requirements: []runinput.Requirement{{
			RequirementID: "req_a", RoleID: reqRole, Level: 2, Criticality: "must", Headcount: 1,
			Effort:         runinput.Effort{MinPersonDays: 1, LikelyPersonDays: 1, MaxPersonDays: 1},
			AllowsExternal: true,
		}},
		Candidates: []runinput.Candidate{{
			EmployeeID: "emp_ab01", Grade: "M", CostCoefficient: cost,
			RoleProfileID: candRole, EmploymentType: "internal",
			CurrentProjectIDs: f.projects,
			Capacity: []runinput.CapacityRow{{
				Week: "2026-W10", WorkingDays: 5, NormHours: 40, FreeHours: f.freeHours,
			}},
		}},
		Constraints: runinput.Constraints{
			MaxUtilizationRatio: 0.95, DeadlineWeek: f.deadline, ForbidExternal: f.forbidExternal,
		},
		CreatedAt: time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("build snapshot: %v", err)
	}
	return snap
}

func deficitResult(deadline string) metrics.VariantResult {
	return metrics.VariantResult{Duration: metrics.VariantDuration{DeadlineWeek: deadline}}
}

var contractDeficitCodes = map[string]bool{
	"DEFICIT_EXTERNAL_CONTRACTOR": true,
	"DEFICIT_MUST_ROLE":           true,
	"DEFICIT_UPSKILL_INTERNAL":    true,
	"DEFICIT_DEADLINE_SHIFT":      true,
	"TRADEOFF_LOAD_VS_SPEED":      true,
}

func assertContractActions(t *testing.T, actions []RecommendedAction) {
	t.Helper()
	for _, a := range actions {
		if len(a.ReasonCodes) == 0 {
			t.Fatalf("action %q has no reason codes", a.Action)
		}
		for _, code := range a.ReasonCodes {
			if !contractDeficitCodes[code] {
				t.Fatalf("action %q has non-contract reason code %q", a.Action, code)
			}
		}
		if a.TimeToStartWeeks != nil {
			t.Fatalf("action %q carries an unsourced time_to_start_weeks", a.Action)
		}
	}
}

func assertCost(t *testing.T, a RecommendedAction, want float64) {
	t.Helper()
	if a.EstimatedCostUnits == nil || math.Abs(*a.EstimatedCostUnits-want) > 1e-6 {
		t.Fatalf("action %q cost = %v, want %v (person-days * 8 h * coefficient)", a.Action, a.EstimatedCostUnits, want)
	}
}

func TestBuildDeficitActionsNoInternalCapacity(t *testing.T) {
	snap := deficitSnapshot(t, deficitFixture{freeHours: 0, cost: 1.4, deadline: "2026-W10"})
	d := metrics.Deficit{RequirementID: "req_a", RoleID: "qa_engineer", PersonDays: 3, ExternalCooperationFlag: true}
	actions := BuildDeficitActions(d, snap, deficitResult("2026-W10"))
	assertContractActions(t, actions)

	if len(actions) != 3 {
		t.Fatalf("actions = %#v, want contractor/hire/move_deadline", actions)
	}
	if actions[0].Action != actionContractor || actions[1].Action != actionHire || actions[2].Action != actionMoveDeadline {
		t.Fatalf("unexpected action order: %#v", actions)
	}
	assertCost(t, actions[0], 3*8*1.4)
	assertCost(t, actions[1], 3*8*1.4)
	if actions[2].EstimatedCostUnits != nil {
		t.Fatalf("move_deadline must not carry a cost: %v", *actions[2].EstimatedCostUnits)
	}
	for i := 0; i < 2; i++ {
		codes := actions[i].ReasonCodes
		if len(codes) != 2 || codes[0] != "DEFICIT_EXTERNAL_CONTRACTOR" || codes[1] != "DEFICIT_MUST_ROLE" {
			t.Fatalf("external action codes = %v", codes)
		}
	}
	if !reflect.DeepEqual(actions[2].ReasonCodes, []string{"DEFICIT_DEADLINE_SHIFT"}) {
		t.Fatalf("deadline action codes = %v", actions[2].ReasonCodes)
	}

	encoded, err := json.Marshal(actions)
	if err != nil {
		t.Fatalf("marshal actions: %v", err)
	}
	var wire []map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("unmarshal actions: %v", err)
	}
	for i, a := range wire {
		if _, ok := a["time_to_start_weeks"]; ok {
			t.Fatalf("action %d serialized an unsourced time_to_start_weeks: %s", i, encoded)
		}
		if _, ok := a["from_project_id"]; ok {
			t.Fatalf("action %d serialized an unsourced from_project_id: %s", i, encoded)
		}
	}
}

func TestBuildDeficitActionsInternalCapacity(t *testing.T) {
	// Build canonicalizes CurrentProjectIDs to ["prj_p0", "prj_p1"].
	snap := deficitSnapshot(t, deficitFixture{
		freeHours: 40, projects: []string{"prj_p1", "prj_p0"}, cost: 1.4, deadline: "2026-W10",
	})
	d := metrics.Deficit{RequirementID: "req_a", RoleID: "qa_engineer", PersonDays: 3, ExternalCooperationFlag: true}
	actions := BuildDeficitActions(d, snap, deficitResult("2026-W10"))
	assertContractActions(t, actions)

	if len(actions) != 2 || actions[0].Action != actionUpskill || actions[1].Action != actionReallocate {
		t.Fatalf("actions = %#v, want upskill/reallocate", actions)
	}
	assertCost(t, actions[0], 3*8*1.4)
	assertCost(t, actions[1], 3*8*1.4)
	if actions[1].FromProjectID != "prj_p0" {
		t.Fatalf("from_project_id = %q, want prj_p0", actions[1].FromProjectID)
	}
	if actions[0].FromProjectID != "" {
		t.Fatalf("upskill must not name a source project: %q", actions[0].FromProjectID)
	}
}

func TestBuildDeficitActionsOmitsCostWithoutQualifiedCoefficient(t *testing.T) {
	snap := deficitSnapshot(t, deficitFixture{
		freeHours: 0, cost: 1.4, deadline: "2026-W10", reqRole: "backend_engineer", candRole: "qa_engineer",
	})
	d := metrics.Deficit{RequirementID: "req_a", RoleID: "backend_engineer", PersonDays: 3, ExternalCooperationFlag: true}
	actions := BuildDeficitActions(d, snap, deficitResult("2026-W10"))
	assertContractActions(t, actions)
	if len(actions) != 3 {
		t.Fatalf("actions = %#v", actions)
	}
	for _, a := range actions {
		if a.EstimatedCostUnits != nil {
			t.Fatalf("action %q invented a cost without a qualified coefficient: %v", a.Action, *a.EstimatedCostUnits)
		}
	}
}

func TestBuildDeficitActionsForbidsExternal(t *testing.T) {
	snap := deficitSnapshot(t, deficitFixture{
		freeHours: 0, cost: 1.4, deadline: "2026-W10", forbidExternal: true,
	})
	d := metrics.Deficit{RequirementID: "req_a", RoleID: "qa_engineer", PersonDays: 3, ExternalCooperationFlag: true}
	actions := BuildDeficitActions(d, snap, deficitResult("2026-W10"))
	assertContractActions(t, actions)
	if len(actions) != 1 || actions[0].Action != actionMoveDeadline {
		t.Fatalf("actions = %#v, want only move_deadline", actions)
	}
}

func TestBuildDeficitActionsDeterministic(t *testing.T) {
	snap := deficitSnapshot(t, deficitFixture{freeHours: 40, projects: []string{"prj_p1"}, cost: 1.4, deadline: "2026-W10"})
	d := metrics.Deficit{RequirementID: "req_a", RoleID: "qa_engineer", PersonDays: 3, ExternalCooperationFlag: true}
	first := BuildDeficitActions(d, snap, deficitResult("2026-W10"))
	for i := 0; i < 5; i++ {
		if got := BuildDeficitActions(d, snap, deficitResult("2026-W10")); !reflect.DeepEqual(got, first) {
			t.Fatalf("non-deterministic actions: %#v vs %#v", got, first)
		}
	}
}
