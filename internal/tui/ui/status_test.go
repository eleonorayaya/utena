package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/stretchr/testify/assert"
)

func TestClaudeGlyph(t *testing.T) {
	assert.Equal(t, "!", ClaudeGlyph(claude.StatusNeedsAttention).Text)
	assert.Equal(t, "✓", ClaudeGlyph(claude.StatusReadyForReview).Text)
	assert.Equal(t, "~", ClaudeGlyph(claude.StatusWorking).Text)
	assert.Equal(t, "✓", ClaudeGlyph(claude.StatusDone).Text)
	assert.Equal(t, "·", ClaudeGlyph(claude.StatusIdle).Text)
	assert.Equal(t, "", ClaudeGlyph("").Text)
	assert.NotEqual(t,
		ClaudeGlyph(claude.StatusReadyForReview).Style.GetForeground(),
		ClaudeGlyph(claude.StatusDone).Style.GetForeground())
}

func TestClaudeStatusText(t *testing.T) {
	assert.Equal(t, "Needs your input", ClaudeStatusText(claude.StatusNeedsAttention))
	assert.Equal(t, "Ready for review", ClaudeStatusText(claude.StatusReadyForReview))
	assert.Equal(t, "Working", ClaudeStatusText(claude.StatusWorking))
	assert.Equal(t, "Idle", ClaudeStatusText(claude.StatusIdle))
	assert.Equal(t, "Done", ClaudeStatusText(claude.StatusDone))
	assert.Equal(t, "", ClaudeStatusText(""))
}

func TestPRBadge(t *testing.T) {
	assert.Contains(t, PRBadge(git.PRStateOpen), "● open")
	assert.Contains(t, PRBadge(git.PRStateDraft), "◌ draft")
	assert.Contains(t, PRBadge(git.PRStateMerged), "⊕ merged")
	assert.Contains(t, PRBadge(git.PRStateClosed), "✕ closed")
}

func TestPRCounts_IgnoresMergedAndClosed(t *testing.T) {
	ready, draft := PRCounts([]git.PullRequest{
		{State: git.PRStateOpen}, {State: git.PRStateOpen},
		{State: git.PRStateDraft},
		{State: git.PRStateMerged}, {State: git.PRStateClosed},
	})
	assert.Equal(t, 2, ready)
	assert.Equal(t, 1, draft)
}

func spansText(spans []Span) string {
	s := ""
	for _, sp := range spans {
		s += sp.Text
	}
	return s
}

func TestPRCountSpans(t *testing.T) {
	assert.Equal(t, "●1 ◌2", spansText(PRCountSpans(1, 2)))
	assert.Equal(t, "●3", spansText(PRCountSpans(3, 0)))
	assert.Equal(t, "◌1", spansText(PRCountSpans(0, 1)))
	assert.Empty(t, PRCountSpans(0, 0))
	assert.LessOrEqual(t, lipgloss.Width(spansText(PRCountSpans(12, 34))), 7)
}

func TestSectionHeader(t *testing.T) {
	assert.Equal(t, "Needs you 2", SectionHeader("Needs you", 2))
}

func TestSessionGlyph(t *testing.T) {
	cases := map[session.SessionStatus]string{
		session.StatusActive:    "●",
		session.StatusCompleted: "✓",
		session.StatusCreating:  "◐",
		session.StatusPending:   "◐",
		session.StatusBroken:    "✕",
		session.StatusInactive:  "○",
		session.StatusArchived:  "○",
		"unmanaged":             "◌",
	}
	for status, want := range cases {
		assert.Equal(t, want, SessionGlyph(status).Text, status)
	}
	assert.Equal(t, theme.Current.Error, SessionStatusColor(session.StatusBroken))
	assert.Equal(t, theme.Current.TextMuted, SessionStatusColor("unmanaged"))
}
