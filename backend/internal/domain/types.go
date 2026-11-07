// Package domain holds core domain types and validation logic.
// No infrastructure dependencies; all errors use %w wrapping.
package domain

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

// ---------- Regex patterns ----------

var (
	EmployeeIDRe = regexp.MustCompile(`^emp_[a-z0-9]{4,12}$`)
	ProjectIDRe  = regexp.MustCompile(`^prj_[a-z0-9]{4,12}$`)
	AssignmentIDRe = regexp.MustCompile(`^asg_[a-z0-9]{4,12}$`)
	AbsenceIDRe  = regexp.MustCompile(`^abs_[a-z0-9]{4,12}$`)
	UnitIDRe     = regexp.MustCompile(`^unt_[a-z0-9]{2,12}$`)
	AuditIDRe    = regexp.MustCompile(`^aud_[a-z0-9]{4,12}$`)
	WeekKeyRe    = regexp.MustCompile(`^[0-9]{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$`)
	RoleIDRe     = regexp.MustCompile(`^[a-z][a-z0-9_]{2,39}$`)
	CompetencyIDRe = regexp.MustCompile(`^[a-z][a-z0-9_]{2,39}$`)
	RequestIDRe  = regexp.MustCompile(`^req_[A-Za-z0-9-]{6,32}$`)
)

// ---------- Problem (RFC 9457) ----------

// Problem is the standard error response shape.
type Problem struct {
	Type        string       `json:"type"`
	Title       string       `json:"title"`
	Status      int          `json:"status"`
	Code        string       `json:"code"`
	Detail      string       `json:"detail"`
	RequestID   string       `json:"request_id"`
	ReasonCodes []string     `json:"reason_codes"`
	Errors      []FieldError `json:"errors,omitempty"`
	Retryable   *bool        `json:"retryable,omitempty"`
}

// FieldError describes a single field-level validation failure.
type FieldError struct {
	Field  string `json:"field"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func (p Problem) Error() string {
	return fmt.Sprintf("[%d] %s: %s", p.Status, p.Code, p.Detail)
}

// ---------- Enums ----------

var (
	Grades         = map[string]bool{"J": true, "M": true, "S": true, "L": true, "P": true}
	EmploymentTypes = map[string]bool{"internal": true, "contractor": true, "external_partner": true}
	ProjectStatuses = map[string]bool{"planned": true, "active": true, "paused": true, "done": true, "cancelled": true}
	ProjectCriticalities = map[string]bool{"critical": true, "high": true, "normal": true, "low": true}
	AbsenceTypes   = map[string]bool{"vacation": true, "sick_leave": true, "training": true, "business_trip": true, "parental_leave": true, "other": true}
	AbsenceSources = map[string]bool{"manual": true, "hr_import": true, "calendar_import": true}
	AssignmentSources = map[string]bool{"plan": true, "actual": true, "forecast": true}
	CompSources    = map[string]bool{"self_assessment": true, "manager_review": true, "project_history": true, "certification": true, "hr_import": true}
)

// ---------- Capacity Profile ----------

type CapacityProfile struct {
	WeeklyHoursNorm    float64 `json:"weekly_hours_norm"`
	ReserveRatio       float64 `json:"reserve_ratio"`
	MaxUtilizationRatio float64 `json:"max_utilization_ratio"`
}

// ---------- Employee ----------

type Employee struct {
	EmployeeID     string          `json:"employee_id"`
	Grade          string          `json:"grade"`
	RoleProfileID  string          `json:"role_profile_id"`
	UnitID         string          `json:"unit_id"`
	EmploymentType string          `json:"employment_type"`
	BenchFlag      bool            `json:"bench_flag"`
	CapacityProfile CapacityProfile `json:"capacity_profile"`
	CostCoefficient float64        `json:"cost_coefficient"`
	Active         bool            `json:"active"`
}

type EmployeeCreate struct {
	EmployeeID      string           `json:"employee_id"`
	Grade           string           `json:"grade"`
	RoleProfileID   string           `json:"role_profile_id"`
	UnitID          string           `json:"unit_id"`
	EmploymentType  string           `json:"employment_type"`
	BenchFlag       *bool            `json:"bench_flag"`
	CapacityProfile *CapacityProfile `json:"capacity_profile"`
	CostCoefficient *float64         `json:"cost_coefficient"`
}

type EmployeeUpdate struct {
	Grade           *string          `json:"grade"`
	RoleProfileID   *string          `json:"role_profile_id"`
	UnitID          *string          `json:"unit_id"`
	EmploymentType  *string          `json:"employment_type"`
	BenchFlag       *bool            `json:"bench_flag"`
	CapacityProfile *CapacityProfile `json:"capacity_profile"`
	Active          *bool            `json:"active"`
}

// ---------- Employee Competency ----------

type EmployeeCompetency struct {
	CompetencyID string      `json:"competency_id"`
	Level        int         `json:"level"`
	Confidence   float64     `json:"confidence"`
	LastUsed     *time.Time  `json:"last_used,omitempty"`
	Source       string      `json:"source"`
}

type EmployeeCompetencyUpsert struct {
	CompetencyID string     `json:"competency_id"`
	Level        int        `json:"level"`
	Confidence   float64    `json:"confidence"`
	LastUsed     *time.Time `json:"last_used,omitempty"`
	Source       string     `json:"source"`
}

// ---------- Employee List Filters ----------

type EmployeeFilter struct {
	RoleID       string
	CompetencyID string
	MinLevel     int
	BenchOnly    bool
	Limit        int
	Offset       int
}

// ---------- Project ----------

type Project struct {
	ProjectID             string `json:"project_id"`
	Code                  string `json:"code"`
	Name                  string `json:"name"`
	Status                string `json:"status"`
	Priority              int    `json:"priority"`
	Criticality           string `json:"criticality"`
	StartWeek             string `json:"start_week"`
	EndWeekPlanned        string `json:"end_week_planned"`
	EndWeekForecast       string `json:"end_week_forecast,omitempty"`
	ReplacesOtherProjects bool   `json:"replaces_other_projects"`
}

type ProjectCreate struct {
	ProjectID             string `json:"project_id"`
	Code                  string `json:"code"`
	Name                  string `json:"name"`
	Status                string `json:"status"`
	Priority              int    `json:"priority"`
	Criticality           string `json:"criticality"`
	StartWeek             string `json:"start_week"`
	EndWeekPlanned        string `json:"end_week_planned"`
	ReplacesOtherProjects *bool  `json:"replaces_other_projects"`
}

type ProjectFilter struct {
	Status string
	Limit  int
	Offset int
}

// ---------- Assignment ----------

type Assignment struct {
	AssignmentID string  `json:"assignment_id"`
	ProjectID    string  `json:"project_id"`
	EmployeeID   string  `json:"employee_id"`
	RoleID       string  `json:"role_id"`
	Week         string  `json:"week"`
	Hours        float64 `json:"hours"`
	Source       string  `json:"source"`
}

type AssignmentCreate struct {
	EmployeeID string  `json:"employee_id"`
	RoleID     string  `json:"role_id"`
	Week       string  `json:"week"`
	Hours      float64 `json:"hours"`
}

type AssignmentCreateBatch struct {
	Assignments       []AssignmentCreate `json:"assignments"`
	OverloadOverride  *OverloadOverride  `json:"overload_override,omitempty"`
}

type OverloadOverride struct {
	Reason string `json:"reason"`
}

type AssignmentFilter struct {
	ProjectID  string
	EmployeeID string
	FromWeek   string
	ToWeek     string
}

// ---------- Absence ----------

type Absence struct {
	AbsenceID    string     `json:"absence_id"`
	EmployeeID   string     `json:"employee_id"`
	Type         string     `json:"type"`
	StartDate    time.Time  `json:"start_date"`
	EndDate      time.Time  `json:"end_date"`
	HoursPerDay  float64    `json:"hours_per_day"`
	Approved     bool       `json:"approved"`
	Source       string     `json:"source"`
}

type AbsenceCreate struct {
	EmployeeID  string    `json:"employee_id"`
	Type        string    `json:"type"`
	StartDate   time.Time `json:"start_date"`
	EndDate     time.Time `json:"end_date"`
	HoursPerDay float64   `json:"hours_per_day"`
	Approved    *bool     `json:"approved"`
	Source      string    `json:"source"`
}

type AbsenceFilter struct {
	EmployeeID string
	From       *time.Time
	To         *time.Time
	Limit      int
	Offset     int
}

// ---------- Audit ----------

type AuditEntry struct {
	EventID    string          `json:"event_id"`
	At         time.Time       `json:"at"`
	Actor      string          `json:"actor"`
	Action     string          `json:"action"`
	EntityType string          `json:"entity_type"`
	EntityID   string          `json:"entity_id"`
	RequestID  string          `json:"request_id"`
	Summary    string          `json:"summary"`
	Details    json.RawMessage `json:"details,omitempty"`
}

// ---------- Page ----------

type PageMeta struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}
