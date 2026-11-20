// Package explain produces deterministic explanations for a team variant strictly from
// reason codes and structured facts (ADR-007 §2, docs/METRICS.md 3.5).
//
// There is no LLM and no free-form number: the text of every factor is rendered from a
// RU/EN template keyed by a canonical ReasonCode, and every number that appears in that
// text is also stored in the factor's `facts` map. The package exposes a pure
// faithfulness checker (Faithfulness) that re-derives every numeric token of a text from
// the facts and fails when any number has no source, so qa-eval can enforce the 100%
// target mechanically.
//
// Coverage is total: every included member, every relevant excluded candidate (a
// candidate whose role profile or competency matches at least one requirement but who is
// not on the team) and every deficit of the variant gets an ExplanationFactor.
// Counterfactuals are verified by rerun whenever a metrics/schedule recomputation can be
// afforded: the package recomputes duration via internal/metrics over the same immutable
// snapshot (possibly derived) and reports before/after values in facts, marking
// `verified_by=rerun`.
package explain

import (
	"github.com/teamopt/backend/internal/metrics"
	"github.com/teamopt/backend/internal/runinput"
)

// MemberDetail is the per-member view of a variant needed to explain and re-derive
// numbers. It is produced by the service from the solver proposal + snapshot.
type MemberDetail struct {
	EmployeeID      string
	RoleID          string
	Grade           string
	AllocatedHours  float64
	AssignmentShare float64
	CompetenceFit   float64
	ReasonCodes     []string
}

// WeeklyAssignment mirrors one atomic plan row.
type WeeklyAssignment struct {
	EmployeeID    string
	RequirementID string
	Week          string
	Hours         float64
	Share         float64
}

// VariantDetail is everything the explainers and the variant endpoint need that is not
// already part of metrics.VariantResult.
type VariantDetail struct {
	VariantID         string
	Members           []MemberDetail
	WeeklyAssignments []WeeklyAssignment
	ReasonCodes       []string
}

// Input is what Generate needs to produce an Explanation.
type Input struct {
	RunID    string
	Locale   string // "ru" or "en"
	Snapshot *runinput.Snapshot
	Variant  metrics.VariantResult
	Detail   VariantDetail
	Schedule metrics.ScheduleConfig
}

// Factor is one fully-rendered explanation element (contract ExplanationFactor).
type Factor struct {
	EmployeeID   *string        `json:"employee_id,omitempty"`
	CompetencyID *string        `json:"competency_id,omitempty"`
	Text         string         `json:"text"`
	ReasonCodes  []string       `json:"reason_codes"`
	Facts        map[string]any `json:"facts"`
}

// Tradeoff mirrors contracts Tradeoff.
type Tradeoff struct {
	Text        string         `json:"text"`
	ReasonCodes []string       `json:"reason_codes"`
	Facts       map[string]any `json:"facts"`
}

// Counterfactual mirrors contracts Counterfactual. VerifiedBy is "rerun" or "estimated".
type Counterfactual struct {
	Text        string         `json:"text"`
	ReasonCodes []string       `json:"reason_codes"`
	VerifiedBy  string         `json:"verified_by"`
	RunID       string         `json:"run_id,omitempty"`
	Facts       map[string]any `json:"facts"`
}

// Explanation mirrors contracts Explanation.
type Explanation struct {
	VariantID           string           `json:"variant_id"`
	RunID               string           `json:"run_id"`
	Locale              string           `json:"locale"`
	Headline            string           `json:"headline"`
	HeadlineReasonCodes []string         `json:"headline_reason_codes"`
	Tradeoff            Tradeoff         `json:"tradeoff"`
	Included            []Factor         `json:"included"`
	Excluded            []Factor         `json:"excluded"`
	Deficits            []Factor         `json:"deficits"`
	Counterfactuals     []Counterfactual `json:"counterfactuals"`
}

// Exclusion is a deterministic, fact-based exclusion verdict for a candidate that is
// relevant but not on the team. It is shared by the variant endpoint (excluded_candidates)
// and the explainer (excluded factors).
type Exclusion struct {
	EmployeeID  string
	ReasonCodes []string
	Facts       map[string]any
}
