package repository

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type stubRows struct {
	rows [][]any
	err  error
	idx  int
}

func (s *stubRows) Close() {}
func (s *stubRows) Err() error {
	return s.err
}
func (s *stubRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (s *stubRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (s *stubRows) Conn() *pgx.Conn                              { return nil }
func (s *stubRows) Values() ([]any, error)                       { return nil, nil }
func (s *stubRows) RawValues() [][]byte                          { return nil }

func (s *stubRows) Next() bool {
	if s.idx >= len(s.rows) {
		return false
	}
	s.idx++
	return true
}

func (s *stubRows) Scan(dest ...any) error {
	row := s.rows[s.idx-1]
	for i, target := range dest {
		switch p := target.(type) {
		case *string:
			*p = row[i].(string)
		case *int:
			*p = row[i].(int)
		default:
			return errors.New("unsupported scan target")
		}
	}
	return nil
}

func TestScanAssignmentsReportsRowsError(t *testing.T) {
	row := []any{"asg_abc123", "prj_test", "emp_ab01", "backend_engineer", "2026-W10", 120, "plan"}
	sentinel := errors.New("stream failed mid-read")

	got, err := scanAssignments(&stubRows{rows: [][]any{row}, err: sentinel})
	if !errors.Is(err, sentinel) {
		t.Fatalf("scanAssignments error = %v, want the rows.Err sentinel", err)
	}
	if got != nil {
		t.Fatalf("truncated result must not be returned: %#v", got)
	}

	got, err = scanAssignments(&stubRows{rows: [][]any{row}})
	if err != nil {
		t.Fatalf("scanAssignments: %v", err)
	}
	if len(got) != 1 || got[0].Hours != 2 {
		t.Fatalf("successful scan = %#v, want one 2-hour assignment", got)
	}
}
