package domain

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"time"
)

// ---------- ID generation ----------

const idChars = "abcdefghijklmnopqrstuvwxyz0123456789"

// GenerateID creates a random ID with the given prefix and suffix length.
// Pattern: {prefix}{lowercase-alphanumeric of length n}.
func GenerateID(prefix string, n int) string {
	b := make([]byte, n)
	for i := range b {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(idChars))))
		b[i] = idChars[idx.Int64()]
	}
	return prefix + string(b)
}

// ---------- Problem constructors ----------

func NewProblem(status int, code, detail, requestID string, reasonCodes []string) Problem {
	typeURL := "https://teamopt.internal/problems/bad-request"
	switch {
	case status == 401:
		typeURL = "https://teamopt.internal/problems/unauthorized"
	case status == 404:
		typeURL = "https://teamopt.internal/problems/not-found"
	case status == 409:
		typeURL = "https://teamopt.internal/problems/conflict"
	case status == 422:
		typeURL = "https://teamopt.internal/problems/validation"
	case status == 500:
		typeURL = "https://teamopt.internal/problems/internal"
	}
	title := "Error"
	switch status {
	case 401:
		title = "Authentication required"
	case 400:
		title = "Malformed request"
	case 404:
		title = "Resource not found"
	case 409:
		title = "Conflict"
	case 422:
		title = "Validation failed"
	case 500:
		title = "Internal server error"
	}
	return Problem{
		Type:        typeURL,
		Title:       title,
		Status:      status,
		Code:        code,
		Detail:      detail,
		RequestID:   requestID,
		ReasonCodes: reasonCodes,
	}
}

func NewValidationProblem(detail, requestID string, errors []FieldError) Problem {
	p := NewProblem(422, "VALIDATION_FAILED", detail, requestID, []string{"VALIDATION_FAILED"})
	p.Errors = errors
	return p
}

// ---------- Validation helpers ----------

func ValidateEmployeeCreate(e EmployeeCreate, requestID string) *Problem {
	var errs []FieldError
	if !EmployeeIDRe.MatchString(e.EmployeeID) {
		errs = append(errs, FieldError{"employee_id", "PATTERN_MISMATCH", "must match emp_[a-z0-9]{4,12}"})
	}
	if !Grades[e.Grade] {
		errs = append(errs, FieldError{"grade", "INVALID_ENUM", "must be one of J, M, S, L, P"})
	}
	if !RoleIDRe.MatchString(e.RoleProfileID) {
		errs = append(errs, FieldError{"role_profile_id", "PATTERN_MISMATCH", "invalid role_id format"})
	}
	if !UnitIDRe.MatchString(e.UnitID) {
		errs = append(errs, FieldError{"unit_id", "PATTERN_MISMATCH", "must match unt_[a-z0-9]{2,12}"})
	}
	if !EmploymentTypes[e.EmploymentType] {
		errs = append(errs, FieldError{"employment_type", "INVALID_ENUM", "must be one of internal, contractor, external_partner"})
	}
	if e.CapacityProfile != nil {
		errs = append(errs, validateCapacityProfile(*e.CapacityProfile, "capacity_profile")...)
	}
	if e.CostCoefficient != nil {
		c := *e.CostCoefficient
		if c < 0.5 || c > 5.0 {
			errs = append(errs, FieldError{"cost_coefficient", "VALUE_OUT_OF_RANGE", "must be between 0.5 and 5.0"})
		}
	}
	if len(errs) > 0 {
		p := NewValidationProblem(fmt.Sprintf("%d field(s) failed validation", len(errs)), requestID, errs)
		return &p
	}
	return nil
}

func ValidateEmployeeUpdate(e EmployeeUpdate, requestID string) *Problem {
	var errs []FieldError
	if e.Grade != nil && !Grades[*e.Grade] {
		errs = append(errs, FieldError{"grade", "INVALID_ENUM", "must be one of J, M, S, L, P"})
	}
	if e.RoleProfileID != nil && !RoleIDRe.MatchString(*e.RoleProfileID) {
		errs = append(errs, FieldError{"role_profile_id", "PATTERN_MISMATCH", "invalid role_id format"})
	}
	if e.UnitID != nil && !UnitIDRe.MatchString(*e.UnitID) {
		errs = append(errs, FieldError{"unit_id", "PATTERN_MISMATCH", "must match unt_[a-z0-9]{2,12}"})
	}
	if e.EmploymentType != nil && !EmploymentTypes[*e.EmploymentType] {
		errs = append(errs, FieldError{"employment_type", "INVALID_ENUM", "must be one of internal, contractor, external_partner"})
	}
	if e.CapacityProfile != nil {
		errs = append(errs, validateCapacityProfile(*e.CapacityProfile, "capacity_profile")...)
	}
	if len(errs) > 0 {
		p := NewValidationProblem(fmt.Sprintf("%d field(s) failed validation", len(errs)), requestID, errs)
		return &p
	}
	return nil
}

func validateCapacityProfile(cp CapacityProfile, prefix string) []FieldError {
	var errs []FieldError
	if cp.WeeklyHoursNorm < 1 || cp.WeeklyHoursNorm > 60 {
		errs = append(errs, FieldError{prefix + ".weekly_hours_norm", "VALUE_OUT_OF_RANGE", "must be between 1 and 60"})
	}
	if cp.ReserveRatio < 0 || cp.ReserveRatio > 0.5 {
		errs = append(errs, FieldError{prefix + ".reserve_ratio", "VALUE_OUT_OF_RANGE", "must be between 0 and 0.5"})
	}
	if cp.MaxUtilizationRatio < 0.5 || cp.MaxUtilizationRatio > 1.0 {
		errs = append(errs, FieldError{prefix + ".max_utilization_ratio", "VALUE_OUT_OF_RANGE", "must be between 0.5 and 1.0"})
	}
	return errs
}

func ValidateCompetencyUpsert(c EmployeeCompetencyUpsert, idx int) FieldError {
	if !CompetencyIDRe.MatchString(c.CompetencyID) {
		return FieldError{
			Field:  fmt.Sprintf("[%d].competency_id", idx),
			Code:   "PATTERN_MISMATCH",
			Detail: "invalid competency_id format",
		}
	}
	if c.Level < 1 || c.Level > 5 {
		return FieldError{
			Field:  fmt.Sprintf("[%d].level", idx),
			Code:   "VALUE_OUT_OF_RANGE",
			Detail: "must be between 1 and 5",
		}
	}
	if c.Confidence < 0 || c.Confidence > 1 {
		return FieldError{
			Field:  fmt.Sprintf("[%d].confidence", idx),
			Code:   "VALUE_OUT_OF_RANGE",
			Detail: "must be between 0 and 1",
		}
	}
	if !CompSources[c.Source] {
		return FieldError{
			Field:  fmt.Sprintf("[%d].source", idx),
			Code:   "INVALID_ENUM",
			Detail: "invalid source value",
		}
	}
	return FieldError{}
}

func ValidateProjectCreate(p ProjectCreate, requestID string) *Problem {
	var errs []FieldError
	if p.ProjectID != "" && !ProjectIDRe.MatchString(p.ProjectID) {
		errs = append(errs, FieldError{"project_id", "PATTERN_MISMATCH", "must match prj_[a-z0-9]{4,12}"})
	}
	if len(p.Code) < 1 || len(p.Code) > 40 {
		errs = append(errs, FieldError{"code", "LENGTH_INVALID", "must be between 1 and 40 characters"})
	}
	if len(p.Name) < 1 || len(p.Name) > 200 {
		errs = append(errs, FieldError{"name", "LENGTH_INVALID", "must be between 1 and 200 characters"})
	}
	if !ProjectStatuses[p.Status] {
		errs = append(errs, FieldError{"status", "INVALID_ENUM", "must be one of planned, active, paused, done, cancelled"})
	}
	if p.Priority < 1 || p.Priority > 5 {
		errs = append(errs, FieldError{"priority", "VALUE_OUT_OF_RANGE", "must be between 1 and 5"})
	}
	if !ProjectCriticalities[p.Criticality] {
		errs = append(errs, FieldError{"criticality", "INVALID_ENUM", "must be one of critical, high, normal, low"})
	}
	if !WeekKeyRe.MatchString(p.StartWeek) {
		errs = append(errs, FieldError{"start_week", "PATTERN_MISMATCH", "must match YYYY-WNN format"})
	}
	if !WeekKeyRe.MatchString(p.EndWeekPlanned) {
		errs = append(errs, FieldError{"end_week_planned", "PATTERN_MISMATCH", "must match YYYY-WNN format"})
	}
	if len(errs) > 0 {
		pp := NewValidationProblem(fmt.Sprintf("%d field(s) failed validation", len(errs)), requestID, errs)
		return &pp
	}
	return nil
}

func ValidateAssignmentCreate(a AssignmentCreate, idx int) FieldError {
	if !EmployeeIDRe.MatchString(a.EmployeeID) {
		return FieldError{
			Field:  fmt.Sprintf("assignments[%d].employee_id", idx),
			Code:   "PATTERN_MISMATCH",
			Detail: "must match emp_[a-z0-9]{4,12}",
		}
	}
	if !ProjectIDRe.MatchString(a.ProjectID) {
		return FieldError{
			Field:  fmt.Sprintf("assignments[%d].project_id", idx),
			Code:   "PATTERN_MISMATCH",
			Detail: "must match prj_[a-z0-9]{4,12}",
		}
	}
	if !RoleIDRe.MatchString(a.RoleID) {
		return FieldError{
			Field:  fmt.Sprintf("assignments[%d].role_id", idx),
			Code:   "PATTERN_MISMATCH",
			Detail: "invalid role_id format",
		}
	}
	if !WeekKeyRe.MatchString(a.Week) {
		return FieldError{
			Field:  fmt.Sprintf("assignments[%d].week", idx),
			Code:   "PATTERN_MISMATCH",
			Detail: "must match YYYY-WNN format",
		}
	}
	if a.Hours <= 0 || a.Hours > 60 {
		return FieldError{
			Field:  fmt.Sprintf("assignments[%d].hours", idx),
			Code:   "VALUE_OUT_OF_RANGE",
			Detail: "must be > 0 and <= 60",
		}
	}
	return FieldError{}
}

func ValidateAbsenceCreate(a AbsenceCreate, requestID string) *Problem {
	var errs []FieldError
	if !EmployeeIDRe.MatchString(a.EmployeeID) {
		errs = append(errs, FieldError{"employee_id", "PATTERN_MISMATCH", "must match emp_[a-z0-9]{4,12}"})
	}
	if !AbsenceTypes[a.Type] {
		errs = append(errs, FieldError{"type", "INVALID_ENUM", "invalid absence type"})
	}
	if a.EndDate.Before(a.StartDate) {
		errs = append(errs, FieldError{"end_date", "DATE_BEFORE_START", "end_date must be >= start_date"})
	}
	if a.HoursPerDay < 0 || a.HoursPerDay > 12 {
		errs = append(errs, FieldError{"hours_per_day", "VALUE_OUT_OF_RANGE", "must be between 0 and 12"})
	}
	if a.Source != "" && !AbsenceSources[a.Source] {
		errs = append(errs, FieldError{"source", "INVALID_ENUM", "invalid source value"})
	}
	if len(errs) > 0 {
		p := NewValidationProblem(fmt.Sprintf("%d field(s) failed validation", len(errs)), requestID, errs)
		return &p
	}
	return nil
}

// ---------- Date helpers ----------

// FormatDate returns a date as YYYY-MM-DD.
func FormatDate(t time.Time) string {
	return t.Format("2006-01-02")
}

// ParseDate parses YYYY-MM-DD into a time.Time (UTC, midnight).
func ParseDate(s string) (time.Time, error) {
	return time.Parse("2006-01-02", s)
}

// ---------- Employee Import Validation ----------

func ValidateEmployeeImportItem(item EmployeeImportItem, idx int) []FieldError {
	var errs []FieldError
	prefix := fmt.Sprintf("employees[%d]", idx)

	if !EmployeeIDRe.MatchString(item.EmployeeID) {
		errs = append(errs, FieldError{prefix + ".employee_id", "PATTERN_MISMATCH", "must match emp_[a-z0-9]{4,12}"})
	}
	if !Grades[item.Grade] {
		errs = append(errs, FieldError{prefix + ".grade", "INVALID_ENUM", "must be one of J, M, S, L, P"})
	}
	if !RoleIDRe.MatchString(item.RoleProfileID) {
		errs = append(errs, FieldError{prefix + ".role_profile_id", "PATTERN_MISMATCH", "invalid role_id format"})
	}
	if !UnitIDRe.MatchString(item.UnitID) {
		errs = append(errs, FieldError{prefix + ".unit_id", "PATTERN_MISMATCH", "must match unt_[a-z0-9]{2,12}"})
	}
	if !EmploymentTypes[item.EmploymentType] {
		errs = append(errs, FieldError{prefix + ".employment_type", "INVALID_ENUM", "must be one of internal, contractor, external_partner"})
	}
	if item.CapacityProfile != nil {
		errs = append(errs, validateCapacityProfile(*item.CapacityProfile, prefix+".capacity_profile")...)
	}
	for i, c := range item.Competencies {
		if fe := ValidateCompetencyUpsert(c, i); fe.Code != "" {
			errs = append(errs, FieldError{prefix + ".competencies[" + fmt.Sprint(i) + "]." + fe.Field, fe.Code, fe.Detail})
		}
	}
	for i, a := range item.Assignments {
		if fe := ValidateAssignmentCreate(a, i); fe.Code != "" {
			errs = append(errs, FieldError{prefix + ".assignments[" + fmt.Sprint(i) + "]." + fe.Field, fe.Code, fe.Detail})
		}
	}
	for i, a := range item.Absences {
		if p := ValidateAbsenceCreate(a, ""); p != nil {
			for _, fe := range p.Errors {
				errs = append(errs, FieldError{prefix + ".absences[" + fmt.Sprint(i) + "]." + fe.Field, fe.Code, fe.Detail})
			}
		}
	}
	return errs
}

func ValidateEmployeeImportRequest(req EmployeeImportRequest, requestID string) *Problem {
	var errs []FieldError

	if req.Mode != "" && req.Mode != "upsert" && req.Mode != "replace" {
		errs = append(errs, FieldError{"mode", "INVALID_ENUM", "must be 'upsert' or 'replace'"})
	}
	if len(req.Employees) == 0 {
		errs = append(errs, FieldError{"employees", "EMPTY_ARRAY", "at least 1 employee required"})
	}
	if len(req.Employees) > 2000 {
		errs = append(errs, FieldError{"employees", "TOO_MANY_ITEMS", "maximum 2000 employees per import"})
	}

	// Check for duplicate employee_ids in the same request
	seen := make(map[string]int)
	for i, emp := range req.Employees {
		if prev, ok := seen[emp.EmployeeID]; ok {
			errs = append(errs, FieldError{
				fmt.Sprintf("employees[%d].employee_id", i),
				"DUPLICATE_IN_REQUEST",
				fmt.Sprintf("duplicate employee_id %s (also at index %d)", emp.EmployeeID, prev),
			})
		}
		seen[emp.EmployeeID] = i
	}

	for i, item := range req.Employees {
		errs = append(errs, ValidateEmployeeImportItem(item, i)...)
	}

	if len(errs) > 0 {
		p := NewValidationProblem(fmt.Sprintf("%d field(s) failed validation", len(errs)), requestID, errs)
		return &p
	}
	return nil
}

// ---------- Week helpers ----------

// WeekKeyRegex is exported for use by other packages.
var WeekKeyRegex = WeekKeyRe

// ValidateWeekKey checks that a string is a valid ISO week key.
func ValidateWeekKey(w string) bool {
	return WeekKeyRe.MatchString(w)
}
