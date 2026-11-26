package domain_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/teamopt/backend/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAbsenceCreateAcceptsContractDateOnly(t *testing.T) {
	payload := []byte(`{"employee_id":"emp_abcd1234","type":"vacation","start_date":"2026-02-09","end_date":"2026-02-13","hours_per_day":8,"approved":true,"source":"hr_import"}`)

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var input domain.AbsenceCreate
	require.NoError(t, decoder.Decode(&input))

	assert.Equal(t, time.Date(2026, 2, 9, 0, 0, 0, 0, time.UTC), input.StartDate)
	assert.Equal(t, time.Date(2026, 2, 13, 0, 0, 0, 0, time.UTC), input.EndDate)
	require.NotNil(t, input.Approved)
	assert.True(t, *input.Approved)
}

func TestAbsenceCreateAcceptsLegacyRFC3339(t *testing.T) {
	payload := []byte(`{"employee_id":"emp_abcd1234","type":"other","start_date":"2026-02-09T00:00:00Z","end_date":"2026-02-09T00:00:00Z","hours_per_day":4}`)

	var input domain.AbsenceCreate
	require.NoError(t, json.Unmarshal(payload, &input))
	assert.Equal(t, 2026, input.StartDate.Year())
	assert.Nil(t, input.Approved)
}

func TestAbsenceMarshalsDateOnly(t *testing.T) {
	absence := domain.Absence{
		AbsenceID:   "abs_abcd1234",
		EmployeeID:  "emp_abcd1234",
		Type:        "vacation",
		StartDate:   time.Date(2026, 2, 9, 0, 0, 0, 0, time.UTC),
		EndDate:     time.Date(2026, 2, 13, 0, 0, 0, 0, time.UTC),
		HoursPerDay: 8,
		Approved:    true,
		Source:      "hr_import",
	}

	encoded, err := json.Marshal(absence)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"start_date":"2026-02-09"`)
	assert.Contains(t, string(encoded), `"end_date":"2026-02-13"`)

	var decoded domain.Absence
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.True(t, decoded.StartDate.Equal(absence.StartDate))
	assert.True(t, decoded.EndDate.Equal(absence.EndDate))
}
