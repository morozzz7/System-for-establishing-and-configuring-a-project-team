package domain

import (
	"encoding/json"
	"time"
)

// This file holds the row records of the workflow tables (brief, run, snapshot,
// variants, decision, explanation, overrides). Payload is the exact contract-shaped
// response Go already computed; the structured columns remain queryable.

// Brief mirrors one row of brief.
type Brief struct {
	BriefID            string
	Title              string
	SourceType         string
	Language           string
	Status             string
	Text               string
	TextSHA256         string
	RequestedStartWeek string
	DeadlineWeek       string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	// Data is the full BriefRecord (requirements, review, diagnostics) encoded as JSON.
	Data json.RawMessage
}

// Run mirrors one row of team_run.
type Run struct {
	RunID             string
	BriefID           string
	ParentRunID       string
	Status            string
	Preset            string
	K                 int
	Seed              int64
	TimeLimitMS       int
	StartWeek         string
	EndWeek           string
	Weights           json.RawMessage
	Constraints       json.RawMessage
	CandidateFilter   json.RawMessage
	SolverStatus      string
	SolveTimeMS       int
	MLStatus          string
	MLAttempts        int
	ValidatorRejected int
	FallbackUsed      bool
	FallbackReason    string
	MetricsVersion    string
	ContractVersion   string
	ReasonCodes       []string
	CreatedAt         time.Time
	FinishedAt        time.Time
	Payload           json.RawMessage
}

// Snapshot mirrors one row of run_snapshot. Data is runinput.Data encoded as JSON.
type Snapshot struct {
	SnapshotID       string
	RunID            string
	SnapshotSHA256   string
	HorizonStart     string
	HorizonEnd       string
	Weights          json.RawMessage
	Constraints      json.RawMessage
	RequirementCount int
	CandidateCount   int
	CapacityRows     int
	Data             json.RawMessage
	Candidates       []SnapshotCandidate
	CreatedAt        time.Time
}

// SnapshotCandidate mirrors one row of run_snapshot_candidate.
type SnapshotCandidate struct {
	EmployeeID      string
	Grade           string
	CostCoefficient float64
	CompetencyIDs   []string
	CapacityDigest  string
	CapacityWeeks   int
}

// Variant mirrors one row of team_variant with all of its children.
type Variant struct {
	VariantID          string
	RunID              string
	Label              string
	GatePass           bool
	GateG1             int
	GateG2             int
	GateG3             int
	GateG4             int
	GateG5             int
	MaxOverloadPct     float64
	Score              float64
	TeamSize           int
	CostUnits          float64
	Duration           json.RawMessage
	MetricValues       json.RawMessage
	MetricDetails      json.RawMessage
	ReasonCodes        []string
	Payload            json.RawMessage
	Members            []VariantMember
	WeeklyAssignments  []VariantWeeklyAssignment
	Deficits           []VariantDeficit
	ActiveConstraints  []VariantActiveConstraint
	ExcludedCandidates []VariantExcludedCandidate
}

// VariantMember mirrors one row of variant_member. Hours are decimal hours; the
// repository stores them as integer minutes.
type VariantMember struct {
	EmployeeID      string
	RoleID          string
	Grade           string
	AllocatedHours  float64
	AssignmentShare float64
	CompetenceFit   float64
	ReasonCodes     []string
}

// VariantWeeklyAssignment mirrors one row of variant_weekly_assignment.
type VariantWeeklyAssignment struct {
	EmployeeID    string
	RequirementID string
	Week          string
	Hours         float64
	Share         float64
}

// VariantDeficit mirrors one row of variant_deficit with its recommended actions.
type VariantDeficit struct {
	CompetencyID            string
	CompetencyNameRU        string
	RequirementIDs          []string
	PersonDays              float64
	Weeks                   float64
	ExternalCooperationFlag bool
	RecommendedActions      []VariantRecommendedAction
}

// VariantRecommendedAction mirrors one row of variant_recommended_action.
type VariantRecommendedAction struct {
	Action             string
	EstimatedCostUnits *float64
	TimeToStartWeeks   *float64
	FromProjectID      string
	ReasonCodes        []string
}

// VariantActiveConstraint mirrors one row of variant_active_constraint.
type VariantActiveConstraint struct {
	Type        string
	Value       json.RawMessage
	Source      string
	ReasonCodes []string
}

// VariantExcludedCandidate mirrors one row of variant_excluded_candidate.
type VariantExcludedCandidate struct {
	EmployeeID  string
	ReasonCodes []string
	Detail      json.RawMessage
}

// Decision mirrors one row of decision. The primary key is run_id: a decision is
// immutable and a second attempt is rejected by the database.
type Decision struct {
	RunID        string
	VariantID    string
	DecidedAt    time.Time
	DecidedBy    string
	Comment      string
	EditsApplied json.RawMessage
	AuditEventID string
}

// Explanation mirrors one row of explanation. Payload is the full contract
// Explanation so read paths need no regeneration.
type Explanation struct {
	ExplanationID   string
	RunID           string
	VariantID       string
	Locale          string
	Headline        string
	Tradeoff        json.RawMessage
	Included        json.RawMessage
	Excluded        json.RawMessage
	Deficits        json.RawMessage
	Counterfactuals json.RawMessage
	Payload         json.RawMessage
	CreatedAt       time.Time
}

// ConstraintOverride mirrors one row of constraint_override.
type ConstraintOverride struct {
	RunID         string
	Type          string
	EmployeeID    string
	RequirementID string
	Value         *float64
	Week          string
	Reason        string
}

// AuditFilter narrows an audit_log listing by entity and pagination.
type AuditFilter struct {
	EntityType string
	EntityID   string
	Limit      int
	Offset     int
}
