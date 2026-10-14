package branchpicker

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/stretchr/testify/assert"
)

func opened() Model {
	m := New()
	m.SetSize(40, 12)
	return m.Open("Branch for web", 5, []string{"zeta", "main", "alpha"}, "main", "main")
}

func TestBranchPicker_LocalSectionSortedWithDefaultMarker(t *testing.T) {
	lines := strings.Split(opened().View(), "\n")
	assert.Contains(t, lines[0], "Branch for web")
	assert.Contains(t, lines[2], "Local 3")
	assert.True(t, strings.HasPrefix(lines[3], "  alpha"))
	assert.True(t, strings.HasPrefix(lines[4], "▌ main"))
	assert.True(t, strings.HasSuffix(strings.TrimRight(lines[4], " "), "default"))
	assert.True(t, strings.HasPrefix(lines[5], "  zeta"))
	assert.NotContains(t, opened().View(), "Remote")
}

func TestBranchPicker_RemoteSectionAfterFetch(t *testing.T) {
	m := opened()
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	assert.IsType(t, FetchRequestedMsg{}, cmd())
	assert.Equal(t, "fetching origin…", m.Status().Text)
	m, _ = m.Update(provider.BranchesFetchedMsg{WorkspaceID: 5, Branches: []provider.BranchRef{{Name: "main"}, {Name: "feature", Remote: true}}})
	out := m.View()
	assert.Contains(t, out, "Local 1")
	assert.Contains(t, out, "Remote 1")
	assert.Contains(t, out, "feature")
	assert.True(t, m.Status().Empty())
}

func TestBranchPicker_IgnoresOtherWorkspaces(t *testing.T) {
	m := opened()
	m, _ = m.Update(provider.BranchesFetchedMsg{WorkspaceID: 9, Branches: []provider.BranchRef{{Name: "other"}}})
	assert.NotContains(t, m.View(), "other")
}

func TestBranchPicker_EnterSelectsSkippingHeaders(t *testing.T) {
	m := opened()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, SelectedMsg{Branch: "alpha"}, cmd())
}
