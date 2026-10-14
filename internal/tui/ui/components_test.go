package ui

import (
	"github.com/eleonorayaya/utena/internal/tui/uitest"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testRow struct{ name string }

func (r testRow) FilterValue() string { return r.name }
func (r testRow) Cells(bool) []Cell   { return []Cell{{Spans: []Span{{Text: r.name}}}} }

func sizedList(items []list.Item, w, h int) list.Model {
	l := NewList()
	l.SetSize(w, h)
	l.SetItems(items)
	SkipUnselectable(&l, 0)
	return l
}

func feed(l list.Model, msg tea.Msg) list.Model {
	l, cmd := UpdateList(l, msg)
	for _, m := range uitest.Drain(cmd) {
		if _, ok := m.(list.FilterMatchesMsg); ok {
			l, _ = UpdateList(l, m)
		}
	}
	return l
}

func down(l list.Model) list.Model {
	l, _ = UpdateList(l, tea.KeyMsg{Type: tea.KeyDown})
	return l
}

func TestListDelegate_OneLinePerRowAtListWidth(t *testing.T) {
	l := sizedList([]list.Item{testRow{"alpha"}, testRow{"beta"}}, 30, 5)
	lines := strings.Split(ListView(l), "\n")
	assert.Equal(t, 30, lipgloss.Width(lines[0]))
	assert.True(t, strings.HasPrefix(lines[0], "▌ alpha"))
	assert.True(t, strings.HasPrefix(lines[1], "  beta"))
	assert.Equal(t, 30, lipgloss.Width(lines[1]))
}

func TestListDelegate_SelectionBackgroundOnlyOnSelectedRow(t *testing.T) {
	var b1, b2 strings.Builder
	l := sizedList([]list.Item{testRow{"a"}, testRow{"b"}}, 20, 4)
	ListDelegate{}.Render(&b1, l, 0, testRow{"a"})
	ListDelegate{}.Render(&b2, l, 1, testRow{"b"})
	assert.Equal(t, 20, lipgloss.Width(b1.String()))
	assert.Equal(t, 20, lipgloss.Width(b2.String()))
	assert.Contains(t, b1.String(), "▌")
	assert.NotContains(t, b2.String(), "▌")
}

func TestList_SkipsSectionHeadersAndSpacers(t *testing.T) {
	l := sizedList([]list.Item{SectionItem{Title: "A", Count: 1}, testRow{"one"}, SpacerItem{}, SectionItem{Title: "B", Count: 1}, testRow{"two"}}, 30, 8)
	assert.Equal(t, 1, l.Index())
	l = down(l)
	assert.Equal(t, 4, l.Index())
	l = down(l)
	assert.Equal(t, 4, l.Index())
	l, _ = UpdateList(l, tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 1, l.Index())
	l, _ = UpdateList(l, tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 1, l.Index())
}

func TestList_FilterLineShownAboveRowsAndHeadersDropped(t *testing.T) {
	l := sizedList([]list.Item{SectionItem{Title: "A", Count: 2}, testRow{"alpha"}, testRow{"beta"}}, 30, 6)
	l = feed(l, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	assert.Equal(t, list.Filtering, l.FilterState())
	for _, r := range "bet" {
		l = feed(l, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	l = feed(l, tea.KeyMsg{Type: tea.KeyEnter})
	out := ListView(l)
	lines := strings.Split(out, "\n")
	assert.Contains(t, lines[0], "/ bet")
	assert.Contains(t, out, "beta")
	assert.NotContains(t, out, "alpha")
	assert.LessOrEqual(t, len(lines), 6)
}

func TestForm_FocusClampsAndLabelColumn(t *testing.T) {
	f := NewForm("New session", 3)
	assert.Equal(t, 0, f.Prev().Focused())
	assert.Equal(t, 2, f.Next().Next().Next().Focused())

	fields := []Field{{Label: "Name", Value: "x"}, {Label: "Workspaces", Value: "a\nb"}, {Label: "Mode", Value: "m"}}
	assert.Equal(t, 12, LabelWidth(fields))
	lines := strings.Split(f.Focus(1).View(fields, 60), "\n")
	assert.Contains(t, lines[0], "New session")
	assert.Equal(t, "", lines[1])
	assert.Equal(t, "  Name        x", lines[2])
	assert.Equal(t, "▌ Workspaces  a", lines[3])
	assert.Equal(t, "              b", lines[4])
}

func TestForm_ErrorUnderItsField(t *testing.T) {
	f := NewForm("T", 2)
	lines := strings.Split(f.View([]Field{{Label: "URL", Value: "x", Error: "required"}, {Label: "Dir", Value: "y"}}, 40), "\n")
	assert.Equal(t, "       required", lines[3])
	assert.Contains(t, lines[4], "Dir")
}

func TestForm_LinesFitWidth(t *testing.T) {
	f := NewForm(strings.Repeat("title", 20), 1)
	out := f.View([]Field{{Label: "Name", Value: strings.Repeat("v", 200), Error: strings.Repeat("e", 200)}}, 30)
	for l := range strings.SplitSeq(out, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(l), 30)
	}
}

func TestChoice_Cycles(t *testing.T) {
	c := Choice{Options: []string{"new branch", "use existing"}}
	c, ok := c.Update(tea.KeyMsg{Type: tea.KeyRight})
	assert.True(t, ok)
	assert.Equal(t, "use existing", c.Value())
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeySpace})
	assert.Equal(t, "new branch", c.Value())
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyLeft})
	assert.Equal(t, "use existing", c.Value())
	_, ok = c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, ok)
	assert.Equal(t, "○ new branch   ● use existing", c.View(false))
}

func TestStepList_GlyphsAndFailedError(t *testing.T) {
	out := StepList([]Step{
		{Label: "Fetch", State: StepDone},
		{Label: "Worktree", State: StepRunning},
		{Label: "Start", State: StepFailed, Error: "boom"},
		{Label: "Later", State: StepPending},
	}, "◌", 40)
	assert.Equal(t, []string{"  ✓ Fetch", "  ◌ Worktree", "  ✕ Start", "    boom", "  · Later"}, strings.Split(out, "\n"))
}

func TestList_SetItemsWhileFilteredKeepsMatchesAndSelection(t *testing.T) {
	l := sizedList([]list.Item{SectionItem{Title: "A", Count: 3}, testRow{"alpha"}, testRow{"beta"}, testRow{"betamax"}}, 30, 8)
	l = feed(l, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	for _, r := range "bet" {
		l = feed(l, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	l = feed(l, tea.KeyMsg{Type: tea.KeyEnter})
	l = down(l)
	assert.Equal(t, "betamax", l.SelectedItem().(testRow).name)

	SetItems(&l, []list.Item{SectionItem{Title: "A", Count: 4}, testRow{"alpha"}, testRow{"beta"}, testRow{"betamax"}, testRow{"gamma"}})
	SelectWhere(&l, func(it list.Item) bool { r, ok := it.(testRow); return ok && r.name == "betamax" })
	assert.Len(t, l.VisibleItems(), 2)
	assert.Equal(t, "betamax", l.SelectedItem().(testRow).name)
}

func TestList_FilteringHighlightsFirstMatchWhenListStartsWithHeader(t *testing.T) {
	l := sizedList([]list.Item{SectionItem{Title: "A", Count: 2}, testRow{"alpha"}, testRow{"zeta"}}, 30, 8)
	l = feed(l, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	for _, r := range "zeta" {
		l = feed(l, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	l = feed(l, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, l.SelectedItem())
	assert.Equal(t, "zeta", l.SelectedItem().(testRow).name)
}

func TestClamp(t *testing.T) {
	assert.Equal(t, 5, lipgloss.Width(Clamp(strings.Repeat("x", 20), 5)))
	assert.Equal(t, "abc", Clamp("abc", 0))
}
