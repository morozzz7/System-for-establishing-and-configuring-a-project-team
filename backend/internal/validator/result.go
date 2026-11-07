package validator

// GateViolations matches contracts/openapi.yaml GateViolations exactly.
// All integer counts must be zero for a variant to be returned to the user.
type GateViolations struct {
	G1                         int     `json:"G1"`
	G2                         int     `json:"G2"`
	G3                         int     `json:"G3"`
	G4                         int     `json:"G4"`
	G5                         int     `json:"G5"`
	MaxOverloadPct             float64 `json:"max_overload_pct,omitempty"`
	UncoveredMustNotInDeficit  int     `json:"uncovered_must_not_in_deficit,omitempty"`
}
