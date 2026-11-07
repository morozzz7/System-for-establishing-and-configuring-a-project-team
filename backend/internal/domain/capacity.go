package domain

// WeeklyCapacity represents an employee's capacity for a single ISO week.
// All values are in minutes. AvailableMinutes is clamped to >= 0.
//
// Formula:
//
//	effective_limit = min(NormMinutes × (1 − ReserveRatio), NormMinutes × MaxUtilizationRatio)
//	AvailableMinutes = max(0, effective_limit − AssignedMinutes − AbsentMinutes)
//	UtilizationRatio = (AssignedMinutes + AbsentMinutes) / NormMinutes
type WeeklyCapacity struct {
	EmployeeID       string  `json:"employee_id"`
	Week             string  `json:"week"`              // ISO week: "2026-W01"
	AvailableMinutes int     `json:"available_minutes"` // minutes available for NEW assignments
	AssignedMinutes  int     `json:"assigned_minutes"`  // minutes already assigned
	AbsentMinutes    int     `json:"absent_minutes"`    // minutes lost to absences
	NormMinutes      int     `json:"norm_minutes"`      // weekly_hours_norm × 60
	UtilizationRatio float64 `json:"utilization_ratio"` // (Assigned + Absent) / Norm
}

// CapacityMatrix is the per-employee capacity breakdown across a range of weeks.
type CapacityMatrix struct {
	EmployeeID string           `json:"employee_id"`
	Weeks      []WeeklyCapacity `json:"weeks"`
}
