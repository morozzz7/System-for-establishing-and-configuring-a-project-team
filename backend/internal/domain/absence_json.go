package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

const absenceDateLayout = "2006-01-02"

// parseAbsenceDate accepts the contract's date-only form and the legacy RFC3339 form still
// emitted by older clients.
func parseAbsenceDate(raw json.RawMessage) (time.Time, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return time.Time{}, err
	}
	if parsed, err := time.Parse(absenceDateLayout, value); err == nil {
		return parsed, nil
	}
	return time.Parse(time.RFC3339, value)
}

func (a *AbsenceCreate) UnmarshalJSON(data []byte) error {
	type plain AbsenceCreate
	var dto struct {
		plain
		StartDate json.RawMessage `json:"start_date"`
		EndDate   json.RawMessage `json:"end_date"`
	}
	if err := json.Unmarshal(data, &dto); err != nil {
		return err
	}
	start, err := parseAbsenceDate(dto.StartDate)
	if err != nil {
		return fmt.Errorf("start_date: %w", err)
	}
	end, err := parseAbsenceDate(dto.EndDate)
	if err != nil {
		return fmt.Errorf("end_date: %w", err)
	}
	*a = AbsenceCreate(dto.plain)
	a.StartDate = start
	a.EndDate = end
	return nil
}

func (a Absence) MarshalJSON() ([]byte, error) {
	type plain Absence
	return json.Marshal(struct {
		plain
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	}{
		plain:     plain(a),
		StartDate: a.StartDate.Format(absenceDateLayout),
		EndDate:   a.EndDate.Format(absenceDateLayout),
	})
}

func (a *Absence) UnmarshalJSON(data []byte) error {
	type plain Absence
	var dto struct {
		plain
		StartDate json.RawMessage `json:"start_date"`
		EndDate   json.RawMessage `json:"end_date"`
	}
	if err := json.Unmarshal(data, &dto); err != nil {
		return err
	}
	start, err := parseAbsenceDate(dto.StartDate)
	if err != nil {
		return fmt.Errorf("start_date: %w", err)
	}
	end, err := parseAbsenceDate(dto.EndDate)
	if err != nil {
		return fmt.Errorf("end_date: %w", err)
	}
	*a = Absence(dto.plain)
	a.StartDate = start
	a.EndDate = end
	return nil
}
