package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEffectiveProjectStatusAtMoscowStart(t *testing.T) {
	start := time.Date(2024, time.December, 29, 21, 0, 0, 0, time.UTC)
	project := Project{Status: "planned", StartWeek: "2025-W01"}
	require.Equal(t, "planned", project.WithEffectiveStatus(start.Add(-time.Second)).Status)
	require.Equal(t, "active", project.WithEffectiveStatus(start).Status)
	project.Status = "cancelled"
	require.Equal(t, "cancelled", project.WithEffectiveStatus(start).Status)
}
