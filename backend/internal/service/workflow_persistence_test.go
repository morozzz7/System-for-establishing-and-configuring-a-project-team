package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teamopt/backend/internal/platform/actor"
)

func TestRecordDecisionDerivesActorAndIsIdempotent(t *testing.T) {
	svc, _, runID, _ := newRecalcService(t)
	ctx := actor.WithContext(context.Background(), "emp_ab01")

	resp, err := svc.RecordDecision(ctx, runID, DecisionInput{
		VariantID: "var_alt1", DecidedBy: "emp_zz99", EditsApplied: []map[string]any{{"type": "force_include"}},
	})
	require.NoError(t, err)
	assert.Equal(t, runID, resp.RunID)
	assert.Equal(t, "var_alt1", resp.VariantID)
	assert.Equal(t, "emp_ab01", resp.DecidedBy, "client-supplied decided_by must be ignored")
	assert.Equal(t, 1, resp.EditsCount)
	assert.True(t, strings.HasPrefix(resp.AuditEventID, "aud_"), resp.AuditEventID)

	decision, ok := svc.GetDecision(runID)
	require.True(t, ok)
	assert.Equal(t, "emp_ab01", decision["decided_by"])
	assert.Equal(t, resp.AuditEventID, decision["audit_event_id"])

	_, err = svc.RecordDecision(ctx, runID, DecisionInput{VariantID: "var_alt1"})
	assert.ErrorIs(t, err, ErrDecisionExists)

	auditPage, ok := svc.ListAudit(context.Background(), runID, 10, 0)
	require.True(t, ok)
	items := auditPage["items"].([]any)
	assert.NotEmpty(t, items)
	found := false
	for _, raw := range items {
		entry := raw.(map[string]any)
		if entry["action"] == "decision_recorded" {
			found = true
			assert.Equal(t, "emp_ab01", entry["actor"])
		}
	}
	assert.True(t, found, "decision_recorded must be in the run audit trail")
}

func TestRecordDecisionDefaultsToSystemActor(t *testing.T) {
	svc, _, runID, _ := newRecalcService(t)
	resp, err := svc.RecordDecision(context.Background(), runID, DecisionInput{
		VariantID: "var_alt1", DecidedBy: "emp_zz99",
	})
	require.NoError(t, err)
	assert.Equal(t, actor.System, resp.DecidedBy)
}

func TestListVariantsUsesStoredRun(t *testing.T) {
	svc, _, runID, _ := newRecalcService(t)
	page, ok := svc.ListVariants(context.Background(), runID, 10, 0)
	require.True(t, ok)
	items := page["items"].([]any)
	assert.Len(t, items, 1)
	meta := page["meta"].(map[string]any)
	assert.Equal(t, float64(1), meta["total"])
	assert.Equal(t, float64(10), meta["limit"])
}
