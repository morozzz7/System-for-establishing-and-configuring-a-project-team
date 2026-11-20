package runinput

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fixtureData returns a small but complete snapshot input. It is anonymized:
// pseudonyms only, rates are coefficients by grade.
func fixtureData() Data {
	exp := 0.8
	langGo := "lang_go"
	return Data{
		RunID:       "run_t0001",
		BriefID:     "brf_t0001",
		Preset:      "balanced",
		K:           2,
		Seed:        42,
		TimeLimitMS: 5000,
		Horizon:     []string{"2026-W10", "2026-W11"},
		Weights: map[string]float64{
			"M1": 0.15, "M2": 0.13, "M3": 0.10, "M4": 0.08, "M5": 0.15, "M6": 0.10,
			"M7": 0.08, "M8": 0.08, "M9": 0.05, "M10": 0.04, "M11": 0.02, "M12": 0.02,
		},
		Requirements: []Requirement{
			{
				RequirementID:  "req_t0001",
				RoleID:         "backend_engineer",
				CompetencyID:   &langGo,
				Level:          3,
				Criticality:    "must",
				Headcount:      1,
				Effort:         Effort{MinPersonDays: 5, LikelyPersonDays: 8, MaxPersonDays: 12},
				AllowsExternal: true,
			},
			{
				RequirementID:  "req_t0002",
				RoleID:         "business_analyst",
				CompetencyID:   nil,
				Level:          2,
				Criticality:    "should",
				Headcount:      1,
				Effort:         Effort{MinPersonDays: 2, LikelyPersonDays: 3, MaxPersonDays: 5},
				AllowsExternal: false,
			},
		},
		Candidates: []Candidate{
			{
				EmployeeID: "emp_t001", Grade: "S", CostCoefficient: 1.9,
				RoleProfileID: "backend_engineer", EmploymentType: "internal", BenchFlag: true,
				ExperienceScore:   &exp,
				CurrentProjectIDs: []string{"prj_t001"},
				CompetencyLevels:  map[string]int{"lang_go": 4},
				Capacity: []CapacityRow{
					{Week: "2026-W11", WorkingDays: 5, NormHours: 40, AssignedHours: 8, FreeHours: 32},
					{Week: "2026-W10", WorkingDays: 5, NormHours: 40, FreeHours: 40},
				},
			},
			{
				EmployeeID: "emp_t002", Grade: "M", CostCoefficient: 1.4,
				RoleProfileID: "business_analyst", EmploymentType: "internal", BenchFlag: false,
				CompetencyLevels: map[string]int{},
				Capacity: []CapacityRow{
					{Week: "2026-W10", WorkingDays: 5, NormHours: 40, FreeHours: 40},
					{Week: "2026-W11", WorkingDays: 5, NormHours: 40, FreeHours: 40},
				},
			},
		},
		Constraints: Constraints{
			MaxTeamSize: 3, MinTeamSize: 1, DeadlineWeek: "2026-W11",
			MaxUtilizationRatio: 0.95,
		},
		Portfolio: []PortfolioProject{
			{ProjectID: "prj_t001", RemainingHours: 80, WeeklyCapacity: 40, Critical: true},
		},
		CreatedAt: time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC),
	}
}

func TestBuildRoundTripsCanonicalJSON(t *testing.T) {
	snap, err := Build(fixtureData())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	canonical, err := snap.CanonicalJSON()
	if err != nil {
		t.Fatalf("canonical json: %v", err)
	}
	if !json.Valid(canonical) {
		t.Fatalf("canonical json is not valid JSON: %s", canonical)
	}
	var roundTrip Data
	if err := json.Unmarshal(canonical, &roundTrip); err != nil {
		t.Fatalf("unmarshal canonical json: %v", err)
	}
	again, err := Build(roundTrip)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if again.SnapshotSHA256() != snap.SnapshotSHA256() {
		t.Fatalf("digest changed after round trip: %s != %s", again.SnapshotSHA256(), snap.SnapshotSHA256())
	}
	if !reflect.DeepEqual(again.Data(), snap.Data()) {
		t.Fatalf("snapshot data changed after round trip")
	}
}

func TestBuildDigestIsDeterministicAndOrderIndependent(t *testing.T) {
	first, err := Build(fixtureData())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	second, err := Build(fixtureData())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if first.SnapshotSHA256() != second.SnapshotSHA256() {
		t.Fatalf("same input must give the same digest")
	}
	if len(first.SnapshotSHA256()) != 64 {
		t.Fatalf("digest must be a 64 char sha256 hex, got %q", first.SnapshotSHA256())
	}

	// Reorder every slice and rebuild the weight map in reverse insertion order:
	// canonical JSON must be identical.
	shuffled := fixtureData()
	shuffled.Requirements[0], shuffled.Requirements[1] = shuffled.Requirements[1], shuffled.Requirements[0]
	shuffled.Candidates[0], shuffled.Candidates[1] = shuffled.Candidates[1], shuffled.Candidates[0]
	shuffled.Candidates[1].Capacity[0], shuffled.Candidates[1].Capacity[1] = shuffled.Candidates[1].Capacity[1], shuffled.Candidates[1].Capacity[0]
	reordered := make(map[string]float64, len(shuffled.Weights))
	for k, v := range shuffled.Weights {
		reordered[k] = v
	}
	shuffled.Weights = reordered
	third, err := Build(shuffled)
	if err != nil {
		t.Fatalf("build shuffled: %v", err)
	}
	if third.SnapshotSHA256() != first.SnapshotSHA256() {
		t.Fatalf("digest must not depend on slice order: %s != %s", third.SnapshotSHA256(), first.SnapshotSHA256())
	}

	// A content change must change the digest.
	changed := fixtureData()
	changed.Candidates[0].CostCoefficient = 2.6
	fourth, err := Build(changed)
	if err != nil {
		t.Fatalf("build changed: %v", err)
	}
	if fourth.SnapshotSHA256() == first.SnapshotSHA256() {
		t.Fatalf("digest must change when the content changes")
	}
}

func TestBuildDeepCopiesInput(t *testing.T) {
	data := fixtureData()
	snap, err := Build(data)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	digest := snap.SnapshotSHA256()

	// Mutating the caller's data must not change the snapshot or its digest.
	data.Horizon[0] = "2030-W01"
	data.Weights["M1"] = 0.99
	data.Requirements[0].Level = 5
	data.Candidates[0].CompetencyLevels["lang_go"] = 1
	data.Candidates[0].Capacity[0].FreeHours = 0
	data.Constraints.MaxTeamSize = 1
	data.Portfolio[0].RemainingHours = 9999

	if got := snap.SnapshotSHA256(); got != digest {
		t.Fatalf("snapshot digest changed after mutating the input: %s != %s", got, digest)
	}
	recomputed, err := snap.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if recomputed != digest {
		t.Fatalf("recomputed digest differs: %s != %s", recomputed, digest)
	}
	if snap.Horizon()[0] != "2026-W10" {
		t.Fatalf("snapshot horizon mutated: %v", snap.Horizon())
	}
	if snap.Weights()["M1"] != 0.15 {
		t.Fatalf("snapshot weights mutated: %v", snap.Weights())
	}
	if snap.Requirements()[0].Level != 3 {
		t.Fatalf("snapshot requirements mutated: %v", snap.Requirements())
	}

	// Snapshot accessors return copies where a copy is cheap.
	weights := snap.Weights()
	weights["M2"] = 0.5
	if snap.Weights()["M2"] == 0.5 {
		t.Fatalf("Weights() must return a copy")
	}
	dataAgain := snap.Data()
	dataAgain.Candidates[0].Capacity[0].FreeHours = 1
	if snap.Data().Candidates[0].Capacity[0].FreeHours == 1 {
		t.Fatalf("Data() must return a deep copy")
	}
}

func TestValidateRejectsInvalidSnapshots(t *testing.T) {
	langGo := "lang_go"
	_ = langGo
	cases := []struct {
		name   string
		mutate func(*Data)
		want   string
	}{
		{"bad run id", func(d *Data) { d.RunID = "9b2e" }, "run_id"},
		{"empty brief", func(d *Data) { d.BriefID = "" }, "brief_id"},
		{"unknown preset", func(d *Data) { d.Preset = "turbo" }, "preset"},
		{"k out of range", func(d *Data) { d.K = 4 }, "k ="},
		{"negative seed", func(d *Data) { d.Seed = -1 }, "seed"},
		{"no horizon", func(d *Data) { d.Horizon = nil }, "horizon"},
		{"bad week", func(d *Data) { d.Horizon[0] = "2026-10" }, "horizon[0]"},
		{"horizon order", func(d *Data) {
			d.Horizon = []string{"2026-W11", "2026-W10"}
			for i := range d.Candidates {
				d.Candidates[i].Capacity = []CapacityRow{
					{Week: "2026-W11", NormHours: 40, FreeHours: 40},
					{Week: "2026-W10", NormHours: 40, FreeHours: 40},
				}
			}
		}, "chronological"},
		{"unknown weight key", func(d *Data) {
			d.Weights["M99"] = 0.0
		}, "weights[M99]"},
		{"zero weight sum", func(d *Data) {
			d.Weights = map[string]float64{"M1": 0}
		}, "sum"},
		{"no requirements", func(d *Data) { d.Requirements = nil }, "requirements must not be empty"},
		{"bad requirement id", func(d *Data) { d.Requirements[0].RequirementID = "" }, "requirement_id"},
		{"bad level", func(d *Data) { d.Requirements[0].Level = 9 }, "level"},
		{"bad criticality", func(d *Data) { d.Requirements[0].Criticality = "maybe" }, "criticality"},
		{"bad effort order", func(d *Data) { d.Requirements[0].Effort.LikelyPersonDays = 1 }, "min <= likely <= max"},
		{"no candidates", func(d *Data) { d.Candidates = nil }, "candidates must not be empty"},
		{"bad employee id", func(d *Data) { d.Candidates[0].EmployeeID = "john" }, "employee_id"},
		{"bad grade", func(d *Data) { d.Candidates[0].Grade = "X" }, "grade"},
		{"bad coefficient", func(d *Data) { d.Candidates[0].CostCoefficient = 9 }, "cost_coefficient"},
		{"bad competency level", func(d *Data) { d.Candidates[0].CompetencyLevels["lang_go"] = 7 }, "competency_levels"},
		{"missing capacity week", func(d *Data) { d.Candidates[0].Capacity = d.Candidates[0].Capacity[:1] }, "missing a row"},
		{"capacity outside horizon", func(d *Data) {
			d.Candidates[0].Capacity[0].Week = "2027-W01"
		}, "outside the horizon"},
		{"free above norm", func(d *Data) { d.Candidates[0].Capacity[0].FreeHours = 60 }, "exceeds norm"},
		{"unavailable with hours", func(d *Data) {
			d.Candidates[0].Capacity[0].Unavailable = true
		}, "unavailable"},
		{"team size inverted", func(d *Data) { d.Constraints.MinTeamSize = 5 }, "exceeds max_team_size"},
		{"deadline outside horizon", func(d *Data) { d.Constraints.DeadlineWeek = "2026-W40" }, "outside the horizon"},
		{"bad max utilization", func(d *Data) { d.Constraints.MaxUtilizationRatio = 0.2 }, "max_utilization_ratio"},
		{"duplicate portfolio", func(d *Data) {
			d.Portfolio = append(d.Portfolio, d.Portfolio[0])
		}, "duplicated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := fixtureData()
			tc.mutate(&data)
			err := data.Validate()
			if err == nil {
				t.Fatalf("expected validation error")
			}
			if !errors.Is(err, ErrInvalidSnapshot) {
				t.Fatalf("error must wrap ErrInvalidSnapshot: %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.want)
			}
			if _, err := Build(data); err == nil {
				t.Fatalf("Build must reject invalid data")
			}
		})
	}
}

func TestValidateAcceptsFixture(t *testing.T) {
	if err := fixtureData().Validate(); err != nil {
		t.Fatalf("fixture must be valid: %v", err)
	}
}

func TestSnapshotLookups(t *testing.T) {
	snap, err := Build(fixtureData())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if c, ok := snap.CandidateByID("emp_t001"); !ok || c.Grade != "S" {
		t.Fatalf("candidate lookup failed: %#v", c)
	}
	if _, ok := snap.CandidateByID("emp_missing"); ok {
		t.Fatalf("unknown candidate must not be found")
	}
	if r, ok := snap.RequirementByID("req_t0002"); !ok || r.CompetencyID != nil || r.IsMust() {
		t.Fatalf("requirement lookup failed: %#v", r)
	}
	if r, _ := snap.RequirementByID("req_t0001"); r.CriticalityWeight() != 1.0 {
		t.Fatalf("must weight must be 1.0, got %v", r.CriticalityWeight())
	}
	if snap.RunID() != "run_t0001" || snap.Preset() != "balanced" || snap.K() != 2 || snap.Seed() != 42 {
		t.Fatalf("accessor mismatch: %#v", snap.Data())
	}
	if snap.CreatedAt().Location() != time.UTC {
		t.Fatalf("created_at must be UTC")
	}
	if c, _ := snap.CandidateByID("emp_t001"); c.CompetencyLevel("lang_go") != 4 || !c.HasRole("backend_engineer") {
		t.Fatalf("candidate helpers mismatch")
	}
}
