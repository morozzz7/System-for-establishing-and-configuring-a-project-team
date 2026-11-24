// Package runinput holds the immutable input snapshot of an optimization run.
//
// The snapshot is the single source of truth for everything the run pipeline sends to
// the ML service (POST /v1/optimize) plus the authoritative fields that Go needs to
// recompute metrics, duration and deficits (ADR-007 §1). It deliberately has no
// database, HTTP or random dependencies: Build deep-copies the caller's data, validates
// it, puts slices into a canonical order and stores a sha256 digest over the canonical
// JSON. Two snapshots built from the same data always have the same digest, so a stored
// digest proves that the inputs did not drift between the moment the solver ran and the
// moment a reviewer re-checks G1/G2 (contracts/openapi.yaml RunInputs.snapshot_sha256).
package runinput

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/teamopt/backend/internal/domain"
)

// ErrInvalidSnapshot is wrapped by every validation error returned by Build/Validate.
var ErrInvalidSnapshot = errors.New("invalid run snapshot")

// Presets accepted by a run (contracts/openapi.yaml PresetName).
var Presets = map[string]bool{
	"fast":       true,
	"economical": true,
	"balanced":   true,
	"low_risk":   true,
}

// localID patterns mirror contracts/openapi.yaml: run_* and requirement identifiers.
var (
	runIDRe         = regexp.MustCompile(`^run_[A-Za-z0-9]{4,16}$`)
	requirementIDRe = regexp.MustCompile(`^[A-Za-z0-9_]{1,40}$`)
)

// CriticalityWeights are the requirement weights of docs/METRICS.md (w_r).
var CriticalityWeights = map[string]float64{
	"must":   1.0,
	"should": 0.5,
	"nice":   0.2,
}

// Effort is a three-point estimate in person-days. Effort is never a single number
// (AGENTS.md hard rule 5).
type Effort struct {
	MinPersonDays    float64 `json:"min_person_days"`
	LikelyPersonDays float64 `json:"likely_person_days"`
	MaxPersonDays    float64 `json:"max_person_days"`
}

// Requirement mirrors one normalized brief requirement sent to the optimizer.
// CompetencyID is nil for a role-only requirement: coverage is then decided by the
// employee's role profile, not by a competency level.
type Requirement struct {
	RequirementID  string  `json:"requirement_id"`
	RoleID         string  `json:"role_id"`
	CompetencyID   *string `json:"competency_id"`
	Level          int     `json:"level"`
	Criticality    string  `json:"criticality"`
	Headcount      int     `json:"headcount"`
	Effort         Effort  `json:"effort"`
	AllowsExternal bool    `json:"allows_external"`
}

// CriticalityWeight returns w_r from docs/METRICS.md (0 for an unknown criticality).
func (r Requirement) CriticalityWeight() float64 { return CriticalityWeights[r.Criticality] }

// IsMust reports whether the requirement is a must requirement (G3/M9).
func (r Requirement) IsMust() bool { return r.Criticality == "must" }

// CapacityRow is one employee-week of the capacity matrix. All hours are decimal hours.
//
// Absences are reported separately from existing assignments because docs/METRICS.md M7
// treats absence as lost time, not as load (ADR-007 §1).
type CapacityRow struct {
	Week          string  `json:"week"`
	WorkingDays   int     `json:"working_days"`
	NormHours     float64 `json:"norm_hours"`
	AssignedHours float64 `json:"assigned_hours"`
	AbsentHours   float64 `json:"absent_hours"`
	FreeHours     float64 `json:"free_hours"`
	Unavailable   bool    `json:"unavailable"`
}

// Candidate is one candidate employee as the optimizer sees it, enriched with the fields
// M4/M6/M7/M10/M11/M12 need. ExperienceScore is nil when the employee has no completed
// project history; M4 is then dropped from the score (docs/METRICS.md M4).
type Candidate struct {
	EmployeeID        string         `json:"employee_id"`
	Grade             string         `json:"grade"`
	CostCoefficient   float64        `json:"cost_coefficient"`
	RoleProfileID     string         `json:"role_profile_id"`
	EmploymentType    string         `json:"employment_type"`
	BenchFlag         bool           `json:"bench_flag"`
	ExperienceScore   *float64       `json:"experience_score"`
	CurrentProjectIDs []string       `json:"current_project_ids"`
	CompetencyLevels  map[string]int `json:"competency_levels"`
	Capacity          []CapacityRow  `json:"capacity"`
}

// ShiftFreeHours returns the free hours of a week (0 when the week is not covered).
func (c Candidate) ShiftFreeHours(week string) float64 {
	for _, row := range c.Capacity {
		if row.Week == week {
			if row.Unavailable {
				return 0
			}
			return row.FreeHours
		}
	}
	return 0
}

// CompetencyLevel returns the employee level in a competency (0 when unknown).
func (c Candidate) CompetencyLevel(competencyID string) int {
	return c.CompetencyLevels[competencyID]
}

// HasRole reports whether the employee's role profile matches roleID.
func (c Candidate) HasRole(roleID string) bool { return roleID != "" && c.RoleProfileID == roleID }

// Constraints are the G4 user constraints of the run (contracts RunConstraints).
type Constraints struct {
	ForceInclude        []string `json:"force_include"`
	ForceExclude        []string `json:"force_exclude"`
	MaxTeamSize         int      `json:"max_team_size"`
	MinTeamSize         int      `json:"min_team_size"`
	BudgetCapUnits      float64  `json:"budget_cap_units"`
	DeadlineWeek        string   `json:"deadline_week"`
	ForbidExternal      bool     `json:"forbid_external"`
	FreezeMembers       []string `json:"freeze_members"`
	MinBusFactor        int      `json:"min_bus_factor"`
	MaxUtilizationRatio float64  `json:"max_utilization_ratio"`
}

// PortfolioProject is one existing project that can slip when people are pulled into the
// proposed team (M8 input).
type PortfolioProject struct {
	ProjectID      string   `json:"project_id"`
	RemainingHours float64  `json:"remaining_hours"`
	WeeklyCapacity float64  `json:"weekly_capacity"`
	Critical       bool     `json:"critical"`
	EmployeeIDs    []string `json:"employee_ids"`
}

// Data is the caller-provided input of Build. Build deep-copies it and never keeps a
// reference, so mutating Data after Build cannot change the snapshot or its digest.
type Data struct {
	RunID        string             `json:"run_id"`
	BriefID      string             `json:"brief_id"`
	Preset       string             `json:"preset"`
	K            int                `json:"k"`
	Seed         int64              `json:"seed"`
	TimeLimitMS  int                `json:"time_limit_ms"`
	Horizon      []string           `json:"horizon_weeks"`
	Weights      map[string]float64 `json:"weights"`
	Requirements []Requirement      `json:"requirements"`
	Candidates   []Candidate        `json:"candidates"`
	Constraints  Constraints        `json:"constraints"`
	Portfolio    []PortfolioProject `json:"portfolio"`
	CreatedAt    time.Time          `json:"created_at"`
}

// Snapshot is an immutable, validated run input. It has no exported mutable state:
// every accessor either returns a value or a read-only view of internal data that
// callers must not modify (the package itself never does).
type Snapshot struct {
	runID        string
	briefID      string
	preset       string
	k            int
	seed         int64
	timeLimitMS  int
	horizon      []string
	weights      map[string]float64
	requirements []Requirement
	candidates   []Candidate
	constraints  Constraints
	portfolio    []PortfolioProject
	createdAt    time.Time
	sha256       string
}

// Build validates data, deep-copies and canonicalizes it, then computes the digest.
// The returned snapshot is never mutated afterwards; all maps/slices are private copies.
func Build(data Data) (*Snapshot, error) {
	if err := data.Validate(); err != nil {
		return nil, err
	}
	cp := data.Clone()
	cp.canonicalize()
	s := &Snapshot{
		runID:        cp.RunID,
		briefID:      cp.BriefID,
		preset:       cp.Preset,
		k:            cp.K,
		seed:         cp.Seed,
		timeLimitMS:  cp.TimeLimitMS,
		horizon:      cp.Horizon,
		weights:      cp.Weights,
		requirements: cp.Requirements,
		candidates:   cp.Candidates,
		constraints:  cp.Constraints,
		portfolio:    cp.Portfolio,
		createdAt:    cp.CreatedAt.UTC(),
	}
	digest, err := s.Digest()
	if err != nil {
		return nil, err
	}
	s.sha256 = digest
	return s, nil
}

// Clone returns a deep copy of the data. Map and slice element order is not
// canonicalized; use it when deriving a new run (recalculation) from a snapshot.
func (d Data) Clone() Data {
	out := Data{
		RunID:        d.RunID,
		BriefID:      d.BriefID,
		Preset:       d.Preset,
		K:            d.K,
		Seed:         d.Seed,
		TimeLimitMS:  d.TimeLimitMS,
		Horizon:      append([]string(nil), d.Horizon...),
		Weights:      make(map[string]float64, len(d.Weights)),
		Requirements: make([]Requirement, len(d.Requirements)),
		Candidates:   make([]Candidate, len(d.Candidates)),
		Constraints:  d.Constraints,
		Portfolio:    append([]PortfolioProject(nil), d.Portfolio...),
		CreatedAt:    d.CreatedAt,
	}
	for k, v := range d.Weights {
		out.Weights[k] = v
	}
	for i, r := range d.Requirements {
		out.Requirements[i] = r
		if r.CompetencyID != nil {
			cid := *r.CompetencyID
			out.Requirements[i].CompetencyID = &cid
		}
	}
	for i, c := range d.Candidates {
		cc := c
		cc.CurrentProjectIDs = append([]string(nil), c.CurrentProjectIDs...)
		cc.Capacity = append([]CapacityRow(nil), c.Capacity...)
		cc.CompetencyLevels = make(map[string]int, len(c.CompetencyLevels))
		for k, v := range c.CompetencyLevels {
			cc.CompetencyLevels[k] = v
		}
		if c.ExperienceScore != nil {
			score := *c.ExperienceScore
			cc.ExperienceScore = &score
		}
		out.Candidates[i] = cc
	}
	out.Constraints.ForceInclude = append([]string(nil), d.Constraints.ForceInclude...)
	out.Constraints.ForceExclude = append([]string(nil), d.Constraints.ForceExclude...)
	out.Constraints.FreezeMembers = append([]string(nil), d.Constraints.FreezeMembers...)
	return out
}

// canonicalize puts every slice into a stable order so that the digest depends on the
// content only. Maps are handled by encoding/json, which sorts keys.
func (d *Data) canonicalize() {
	sort.Strings(d.Horizon)
	// Weeks are ISO keys, so lexicographic order is chronological order.
	for i := range d.Candidates {
		sort.Slice(d.Candidates[i].Capacity, func(a, b int) bool {
			return d.Candidates[i].Capacity[a].Week < d.Candidates[i].Capacity[b].Week
		})
		sort.Strings(d.Candidates[i].CurrentProjectIDs)
	}
	sort.Slice(d.Requirements, func(a, b int) bool {
		return d.Requirements[a].RequirementID < d.Requirements[b].RequirementID
	})
	sort.Slice(d.Candidates, func(a, b int) bool {
		return d.Candidates[a].EmployeeID < d.Candidates[b].EmployeeID
	})
	sort.Slice(d.Portfolio, func(a, b int) bool {
		return d.Portfolio[a].ProjectID < d.Portfolio[b].ProjectID
	})
	sort.Strings(d.Constraints.ForceInclude)
	sort.Strings(d.Constraints.ForceExclude)
	sort.Strings(d.Constraints.FreezeMembers)
}

// Digest returns the sha256 over the canonical JSON of the snapshot. It recomputes the
// digest from the current internal state, so it also detects accidental mutation of a
// slice that escaped through an accessor.
func (s *Snapshot) Digest() (string, error) {
	canonical, err := s.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// SnapshotSHA256 returns the digest computed by Build (contracts RunInputs.snapshot_sha256).
func (s *Snapshot) SnapshotSHA256() string { return s.sha256 }

// CanonicalJSON returns the canonical JSON encoding used for the digest: fixed struct
// field order, sorted map keys, slices in canonical order, UTC RFC3339 timestamps.
func (s *Snapshot) CanonicalJSON() ([]byte, error) {
	data := Data{
		RunID:        s.runID,
		BriefID:      s.briefID,
		Preset:       s.preset,
		K:            s.k,
		Seed:         s.seed,
		TimeLimitMS:  s.timeLimitMS,
		Horizon:      s.horizon,
		Weights:      s.weights,
		Requirements: s.requirements,
		Candidates:   s.candidates,
		Constraints:  s.constraints,
		Portfolio:    s.portfolio,
		CreatedAt:    s.createdAt,
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical snapshot json: %w", err)
	}
	return encoded, nil
}

// Data returns a deep copy of the snapshot content (for persistence and tests).
func (s *Snapshot) Data() Data {
	return Data{
		RunID:        s.runID,
		BriefID:      s.briefID,
		Preset:       s.preset,
		K:            s.k,
		Seed:         s.seed,
		TimeLimitMS:  s.timeLimitMS,
		Horizon:      s.horizon,
		Weights:      s.weights,
		Requirements: s.requirements,
		Candidates:   s.candidates,
		Constraints:  s.constraints,
		Portfolio:    s.portfolio,
		CreatedAt:    s.createdAt,
	}.Clone()
}

// RunID returns the run identifier the snapshot belongs to.
func (s *Snapshot) RunID() string { return s.runID }

// BriefID returns the brief the run was created for.
func (s *Snapshot) BriefID() string { return s.briefID }

// Preset returns the weight preset name (fast, economical, balanced, low_risk).
func (s *Snapshot) Preset() string { return s.preset }

// K returns the requested number of variants.
func (s *Snapshot) K() int { return s.k }

// Seed returns the fixed seed used by the solver and the schedule engine.
func (s *Snapshot) Seed() int64 { return s.seed }

// TimeLimitMS returns the solver time limit.
func (s *Snapshot) TimeLimitMS() int { return s.timeLimitMS }

// Horizon returns the planning horizon (ISO weeks, chronological). The slice is
// read-only: callers must not append to or modify it.
func (s *Snapshot) Horizon() []string { return s.horizon }

// Weights returns a copy of the preset weights (M1..M12).
func (s *Snapshot) Weights() map[string]float64 {
	out := make(map[string]float64, len(s.weights))
	for k, v := range s.weights {
		out[k] = v
	}
	return out
}

// Requirements returns the requirements of the run. The slice is read-only.
func (s *Snapshot) Requirements() []Requirement { return s.requirements }

// Candidates returns the candidates of the run. The slice is read-only.
func (s *Snapshot) Candidates() []Candidate { return s.candidates }

// CandidateByID looks up a candidate by pseudonym.
func (s *Snapshot) CandidateByID(employeeID string) (Candidate, bool) {
	for _, c := range s.candidates {
		if c.EmployeeID == employeeID {
			return c, true
		}
	}
	return Candidate{}, false
}

// RequirementByID looks up a requirement by id.
func (s *Snapshot) RequirementByID(requirementID string) (Requirement, bool) {
	for _, r := range s.requirements {
		if r.RequirementID == requirementID {
			return r, true
		}
	}
	return Requirement{}, false
}

// Constraints returns the G4 constraints of the run.
func (s *Snapshot) Constraints() Constraints { return s.constraints }

// Portfolio returns the portfolio projects used by M8. The slice is read-only.
func (s *Snapshot) Portfolio() []PortfolioProject { return s.portfolio }

// CreatedAt returns the snapshot creation time (UTC).
func (s *Snapshot) CreatedAt() time.Time { return s.createdAt }

// ---------- validation ----------

// Validate checks the invariants every consumer of the snapshot relies on. Errors are
// joined and wrap ErrInvalidSnapshot so callers can use errors.Is.
func (d Data) Validate() error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("%w: %s", ErrInvalidSnapshot, fmt.Sprintf(format, args...)))
	}

	if !runIDRe.MatchString(d.RunID) {
		fail("run_id %q must match %s", d.RunID, runIDRe)
	}
	if strings.TrimSpace(d.BriefID) == "" {
		fail("brief_id must not be empty")
	}
	if !Presets[d.Preset] {
		fail("preset %q must be one of fast, economical, balanced, low_risk", d.Preset)
	}
	if d.K < 1 || d.K > 3 {
		fail("k = %d must be in 1..3", d.K)
	}
	if d.Seed < 0 {
		fail("seed = %d must be >= 0", d.Seed)
	}
	if d.TimeLimitMS < 0 {
		fail("time_limit_ms = %d must be >= 0", d.TimeLimitMS)
	}
	if d.CreatedAt.IsZero() {
		fail("created_at must be set")
	}

	if len(d.Horizon) == 0 {
		fail("horizon must contain at least one ISO week")
	}
	seenWeek := make(map[string]bool, len(d.Horizon))
	prev := ""
	for i, week := range d.Horizon {
		if !domain.ValidateWeekKey(week) {
			fail("horizon[%d] = %q is not an ISO week key", i, week)
			continue
		}
		if seenWeek[week] {
			fail("horizon[%d] = %q is duplicated", i, week)
		}
		if prev != "" && week <= prev {
			fail("horizon[%d] = %q is not in chronological order (previous %q)", i, week, prev)
		}
		seenWeek[week] = true
		prev = week
	}

	if len(d.Weights) == 0 {
		fail("weights must not be empty")
	}
	sum := 0.0
	for key, weight := range d.Weights {
		if !metricKeys[key] {
			fail("weights[%s] is not a known metric key (M1..M13)", key)
		}
		if weight < 0 || weight > 1 {
			fail("weights[%s] = %v must be in [0,1]", key, weight)
		}
		sum += weight
	}
	if sum <= 0 {
		fail("weights must sum to a positive value")
	}

	errs = append(errs, d.validateRequirements()...)
	errs = append(errs, d.validateCandidates(seenWeek)...)
	errs = append(errs, d.validateConstraints(seenWeek)...)

	seenProject := make(map[string]bool, len(d.Portfolio))
	for i, p := range d.Portfolio {
		if strings.TrimSpace(p.ProjectID) == "" {
			fail("portfolio[%d].project_id must not be empty", i)
		}
		if seenProject[p.ProjectID] {
			fail("portfolio[%d].project_id %q is duplicated", i, p.ProjectID)
		}
		seenProject[p.ProjectID] = true
		if p.RemainingHours < 0 {
			fail("portfolio[%d].remaining_hours = %v must be >= 0", i, p.RemainingHours)
		}
		if p.WeeklyCapacity < 0 {
			fail("portfolio[%d].weekly_capacity = %v must be >= 0", i, p.WeeklyCapacity)
		}
	}

	return errors.Join(errs...)
}

func (d Data) validateRequirements() []error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("%w: %s", ErrInvalidSnapshot, fmt.Sprintf(format, args...)))
	}
	if len(d.Requirements) == 0 {
		fail("requirements must not be empty")
	}
	seen := make(map[string]bool, len(d.Requirements))
	for i, r := range d.Requirements {
		if !requirementIDRe.MatchString(r.RequirementID) {
			fail("requirements[%d].requirement_id %q must match %s", i, r.RequirementID, requirementIDRe)
		}
		if seen[r.RequirementID] {
			fail("requirements[%d].requirement_id %q is duplicated", i, r.RequirementID)
		}
		seen[r.RequirementID] = true
		if !domain.RoleIDRe.MatchString(r.RoleID) {
			fail("requirements[%d].role_id %q must match %s", i, r.RoleID, domain.RoleIDRe)
		}
		if r.CompetencyID != nil && !domain.CompetencyIDRe.MatchString(*r.CompetencyID) {
			fail("requirements[%d].competency_id %q must match %s", i, *r.CompetencyID, domain.CompetencyIDRe)
		}
		if r.Level < 1 || r.Level > 5 {
			fail("requirements[%d].level = %d must be in 1..5", i, r.Level)
		}
		if _, ok := CriticalityWeights[r.Criticality]; !ok {
			fail("requirements[%d].criticality %q must be must, should or nice", i, r.Criticality)
		}
		if r.Headcount < 1 {
			fail("requirements[%d].headcount = %d must be >= 1", i, r.Headcount)
		}
		e := r.Effort
		if e.MinPersonDays <= 0 || e.LikelyPersonDays <= 0 || e.MaxPersonDays <= 0 {
			fail("requirements[%d].effort must be positive in all three points", i)
		}
		if e.MinPersonDays > e.LikelyPersonDays || e.LikelyPersonDays > e.MaxPersonDays {
			fail("requirements[%d].effort must satisfy min <= likely <= max", i)
		}
	}
	return errs
}

func (d Data) validateCandidates(horizon map[string]bool) []error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("%w: %s", ErrInvalidSnapshot, fmt.Sprintf(format, args...)))
	}
	if len(d.Candidates) == 0 {
		fail("candidates must not be empty")
	}
	seen := make(map[string]bool, len(d.Candidates))
	for i, c := range d.Candidates {
		if !domain.EmployeeIDRe.MatchString(c.EmployeeID) {
			fail("candidates[%d].employee_id %q must match %s", i, c.EmployeeID, domain.EmployeeIDRe)
		}
		if seen[c.EmployeeID] {
			fail("candidates[%d].employee_id %q is duplicated", i, c.EmployeeID)
		}
		seen[c.EmployeeID] = true
		if !domain.Grades[c.Grade] {
			fail("candidates[%d].grade %q must be one of J, M, S, L, P", i, c.Grade)
		}
		if c.CostCoefficient < 0.5 || c.CostCoefficient > 5 {
			fail("candidates[%d].cost_coefficient %v must be in [0.5,5]", i, c.CostCoefficient)
		}
		if !domain.RoleIDRe.MatchString(c.RoleProfileID) {
			fail("candidates[%d].role_profile_id %q must match %s", i, c.RoleProfileID, domain.RoleIDRe)
		}
		if !domain.EmploymentTypes[c.EmploymentType] {
			fail("candidates[%d].employment_type %q is not a known employment type", i, c.EmploymentType)
		}
		if c.ExperienceScore != nil && (*c.ExperienceScore < 0 || *c.ExperienceScore > 1) {
			fail("candidates[%d].experience_score %v must be in [0,1]", i, *c.ExperienceScore)
		}
		for compID, level := range c.CompetencyLevels {
			if !domain.CompetencyIDRe.MatchString(compID) {
				fail("candidates[%d].competency_levels[%s] is not a valid competency id", i, compID)
			}
			if level < 1 || level > 5 {
				fail("candidates[%d].competency_levels[%s] = %d must be in 1..5", i, compID, level)
			}
		}

		rows := make(map[string]bool, len(c.Capacity))
		for j, row := range c.Capacity {
			if !domain.ValidateWeekKey(row.Week) {
				fail("candidates[%d].capacity[%d].week %q is not an ISO week key", i, j, row.Week)
				continue
			}
			if !horizon[row.Week] {
				fail("candidates[%d].capacity[%d].week %q is outside the horizon", i, j, row.Week)
			}
			if rows[row.Week] {
				fail("candidates[%d].capacity[%d].week %q is duplicated", i, j, row.Week)
			}
			rows[row.Week] = true
			if row.WorkingDays < 0 || row.WorkingDays > 5 {
				fail("candidates[%d].capacity[%d].working_days = %d must be in 0..5", i, j, row.WorkingDays)
			}
			if row.NormHours < 0 || row.AssignedHours < 0 || row.AbsentHours < 0 || row.FreeHours < 0 {
				fail("candidates[%d].capacity[%d] hours must be >= 0", i, j)
			}
			if row.FreeHours > row.NormHours && row.NormHours > 0 {
				fail("candidates[%d].capacity[%d].free_hours %v exceeds norm_hours %v", i, j, row.FreeHours, row.NormHours)
			}
			if row.Unavailable && row.FreeHours != 0 {
				fail("candidates[%d].capacity[%d] is unavailable but reports %v free hours", i, j, row.FreeHours)
			}
		}
		for _, week := range d.Horizon {
			if !rows[week] {
				fail("candidates[%d].capacity is missing a row for horizon week %q", i, week)
			}
		}
	}
	return errs
}

func (d Data) validateConstraints(horizon map[string]bool) []error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("%w: %s", ErrInvalidSnapshot, fmt.Sprintf(format, args...)))
	}
	c := d.Constraints
	for _, id := range append(append(append([]string{}, c.ForceInclude...), c.ForceExclude...), c.FreezeMembers...) {
		if !domain.EmployeeIDRe.MatchString(id) {
			fail("constraints employee id %q must match %s", id, domain.EmployeeIDRe)
		}
	}
	if c.MaxTeamSize < 0 {
		fail("constraints.max_team_size = %d must be >= 0", c.MaxTeamSize)
	}
	if c.MinTeamSize < 0 {
		fail("constraints.min_team_size = %d must be >= 0", c.MinTeamSize)
	}
	if c.MaxTeamSize > 0 && c.MinTeamSize > c.MaxTeamSize {
		fail("constraints.min_team_size %d exceeds max_team_size %d", c.MinTeamSize, c.MaxTeamSize)
	}
	if c.BudgetCapUnits < 0 {
		fail("constraints.budget_cap_units = %v must be >= 0", c.BudgetCapUnits)
	}
	if c.DeadlineWeek != "" {
		if !domain.ValidateWeekKey(c.DeadlineWeek) {
			fail("constraints.deadline_week %q is not an ISO week key", c.DeadlineWeek)
		} else if !horizon[c.DeadlineWeek] {
			fail("constraints.deadline_week %q is outside the horizon", c.DeadlineWeek)
		}
	}
	if c.MinBusFactor < 0 || c.MinBusFactor > 5 {
		fail("constraints.min_bus_factor = %d must be in 0..5", c.MinBusFactor)
	}
	if c.MaxUtilizationRatio != 0 && (c.MaxUtilizationRatio < 0.5 || c.MaxUtilizationRatio > 1) {
		fail("constraints.max_utilization_ratio = %v must be in [0.5,1]", c.MaxUtilizationRatio)
	}
	return errs
}

// metricKeys are the composite-score components of docs/METRICS.md.
var metricKeys = map[string]bool{
	"M1": true, "M2": true, "M3": true, "M4": true, "M5": true, "M6": true,
	"M7": true, "M8": true, "M9": true, "M10": true, "M11": true, "M12": true, "M13": true,
}
