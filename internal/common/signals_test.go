package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTopSeverity_MixedList(t *testing.T) {
	signals := []Signal{
		{Source: "ci", Key: "build", Severity: SeverityInfo, Label: "passing"},
		{Source: "pr", Key: "review", Severity: SeverityUrgent, Label: "changes requested"},
		{Source: "ci", Key: "lint", Severity: SeverityWarning, Label: "warnings"},
		{Source: "pr", Key: "approved", Severity: SeveritySuccess, Label: "approved"},
	}
	require.Equal(t, SeverityUrgent, TopSeverity(signals))
}

func TestTopSeverity_EmptyList(t *testing.T) {
	require.Equal(t, SignalSeverity(""), TopSeverity(nil))
	require.Equal(t, SignalSeverity(""), TopSeverity([]Signal{}))
}

func TestSortSignals_Ordering(t *testing.T) {
	signals := []Signal{
		{Source: "a", Key: "1", Severity: SeverityInfo},
		{Source: "b", Key: "2", Severity: SeverityUrgent},
		{Source: "c", Key: "3", Severity: SeverityActive},
		{Source: "d", Key: "4", Severity: SeverityWarning},
		{Source: "e", Key: "5", Severity: SeveritySuccess},
	}
	SortSignals(signals)

	expected := []SignalSeverity{
		SeverityUrgent,
		SeverityWarning,
		SeveritySuccess,
		SeverityActive,
		SeverityInfo,
	}
	for i, s := range signals {
		require.Equal(t, expected[i], s.Severity, "index %d", i)
	}
}
