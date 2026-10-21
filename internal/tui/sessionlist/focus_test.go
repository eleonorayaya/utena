package sessionlist

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/eleonorayaya/utena/internal/bundles"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
	"github.com/eleonorayaya/utena/internal/tui/uitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func press(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	return m.Update(k)
}

func tab() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyTab} }

func blocked(id uint, name string) session.Session {
	s := withClaude(id, name, claude.StatusIdle)
	s.Blockers = []session.SessionBlocker{
		{Model: gorm.Model{ID: 10}, Kind: session.BlockerKindReview, Note: "first"},
		{Model: gorm.Model{ID: 11}, Kind: session.BlockerKindReview, Note: "second"},
	}
	return s
}

func focusedList(t *testing.T, ss ...session.Session) Model {
	t.Helper()
	m := listWith(t, ss...)
	m, _ = press(m, tab())
	require.True(t, m.detail.Focused())
	return m
}

func (m Model) blurred() Model {
	m.detail = m.detail.Blur()
	return m
}

func feed(m Model, cmd tea.Cmd) Model {
	for _, msg := range uitest.Drain(cmd) {
		m, _ = m.Update(msg)
	}
	return m
}

func TestFocus_TabTogglesDetailFocus(t *testing.T) {
	m := focusedList(t, withClaude(1, "a", claude.StatusIdle))
	m, cmd := press(m, tab())
	m = feed(m, cmd)
	assert.False(t, m.detail.Focused())

	m, _ = press(m, tab())
	m, cmd = press(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = feed(m, cmd)
	assert.False(t, m.detail.Focused())
}

func TestFocus_TabWithoutSessionsIsIgnored(t *testing.T) {
	m := listWith(t)
	m, _ = press(m, tab())
	assert.False(t, m.detail.Focused())
}

func TestFocus_NavigationMovesDetailCursorNotList(t *testing.T) {
	m := focusedList(t, blocked(1, "a"), blocked(2, "b"))
	m, _ = press(m, uitest.Runes("j"))
	assert.Equal(t, "a", m.selectedSession().Name)
	assert.Contains(t, ansi.Strip(m.View()), "▌ review · second")
}

func TestFocus_UnfocusedPanelHasNoCursor(t *testing.T) {
	m := listWith(t, blocked(1, "a"))
	assert.NotContains(t, ansi.Strip(m.View()), "▌ review")
}

func TestFocus_ListCursorMovesDetailSelection(t *testing.T) {
	m := listWith(t, withClaude(1, "a", claude.StatusIdle), withClaude(2, "b", claude.StatusIdle))
	m, _ = press(m, uitest.Runes("j"))
	assert.Equal(t, uint(2), m.detail.SessionID())
}

func TestFocus_KeysRouteToDetail(t *testing.T) {
	m := focusedList(t, withClaude(1, "a", claude.StatusIdle))
	_, cmd := press(m, uitest.Runes("w"))
	assert.Equal(t, []string{"tea.sequenceMsg"}, uitest.MsgTypes(cmd))
	_, cmd = press(m.blurred(), uitest.Runes("w"))
	assert.Equal(t, []string{"router.NavigateToMsg"}, uitest.MsgTypes(cmd))

	m, cmd = press(m, uitest.Runes("a"))
	assert.Equal(t, []string{"provider.archiveSessionIntentMsg"}, uitest.MsgTypes(cmd))
	assert.False(t, m.detail.Focused())
}

func TestFocus_ArchiveBlursAndSelectionMoves(t *testing.T) {
	a, b := withClaude(1, "a", claude.StatusIdle), withClaude(2, "b", claude.StatusIdle)
	m := focusedList(t, a, b)
	m, cmd := press(m, uitest.Runes("a"))
	require.NotNil(t, cmd)
	assert.False(t, m.detail.Focused())

	a.Status = session.StatusArchived
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{a, b}})
	assert.Equal(t, uint(2), m.detail.SessionID())
}

func TestFocus_SelectionChangeFromRefreshBlurs(t *testing.T) {
	a, b := withClaude(1, "a", claude.StatusIdle), withClaude(2, "b", claude.StatusIdle)
	m := focusedList(t, a, b)
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{b}})
	assert.False(t, m.detail.Focused())
	assert.Equal(t, uint(2), m.detail.SessionID())
}

func TestFocus_RefreshOfSameSessionKeepsFocusAndCursor(t *testing.T) {
	s := blocked(1, "a")
	m := focusedList(t, s)
	m, _ = press(m, uitest.Runes("j"))
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{s}})
	assert.True(t, m.detail.Focused())
	assert.Contains(t, ansi.Strip(m.View()), "▌ review · second")
}

func TestFocus_SurvivesReInit(t *testing.T) {
	m := focusedList(t, withClaude(1, "a", claude.StatusIdle))
	m, _ = m.Init()
	assert.True(t, m.detail.Focused())
}

func TestFocus_KeysAndStatusComeFromDetail(t *testing.T) {
	m := focusedList(t, withClaude(1, "a", claude.StatusIdle))
	var got []string
	for _, b := range m.Keys().ShortHelp() {
		got = append(got, b.Help().Desc)
	}
	assert.Contains(t, got, "sessions")
	assert.NotContains(t, got, "new")

	m, _ = press(m, uitest.Runes("o"))
	assert.Equal(t, "select a tab or pull request to open", m.Status().Text)

	m, _ = m.Update(outdatedState())
	m, _ = press(m, uitest.Runes("j"))
	assert.Contains(t, m.Status().Text, "browser extension outdated")
}

func TestFocus_CapturesTextWhileFilteringBundles(t *testing.T) {
	m := focusedList(t, withClaude(1, "a", claude.StatusIdle))
	m, _ = m.Update(provider.BundlesLoadedMsg{Bundles: []bundles.Entry{{Name: "go"}}})
	m, cmd := press(m, uitest.Runes("p"))
	m = feed(m, cmd)
	assert.False(t, m.CapturesText())
	m, cmd = press(m, uitest.Runes("/"))
	m = feed(m, cmd)
	assert.True(t, m.CapturesText())

	m, _ = press(m, tab())
	assert.True(t, m.detail.Focused())
}

func TestFocus_DividerHighlightsAndListCursorMutes(t *testing.T) {
	m := listWith(t, withClaude(1, "a", claude.StatusIdle))
	assert.Equal(t, theme.Current.SurfaceVariant, m.dividerColor())
	assert.Equal(t, theme.Current.Primary, m.cursorColor())

	m, _ = press(m, tab())
	assert.Equal(t, theme.Current.Primary, m.dividerColor())
	assert.Equal(t, theme.Current.TextMuted, m.cursorColor())
}

func TestFocus_NarrowWindowShowsDetailFullWidth(t *testing.T) {
	m, _ := New().Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{blocked(1, "narrow")}})
	assert.NotContains(t, m.View(), "Blocked 2")

	m, _ = press(m, tab())
	out := m.View()
	assert.Contains(t, out, "Blocked 2")
	assert.Contains(t, ansi.Strip(out), "▌ review · first")
	lines := strings.Split(out, "\n")
	assert.Len(t, lines, 20)
	for _, l := range lines {
		assert.LessOrEqual(t, lipgloss.Width(l), 60)
	}
}

func TestFocus_ErrorShowsWhileFocusedAndClearsOnNextKey(t *testing.T) {
	m := focusedList(t, withClaude(1, "a", claude.StatusIdle))
	m, _ = m.Update(ui.OpenURLFailedMsg{Err: errors.New("boom")})
	assert.Equal(t, ui.Status{Text: "could not open PR: boom", Kind: ui.StatusError}, m.Status())

	m, _ = press(m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, m.detail.Focused())
	assert.True(t, m.Status().Empty())
}
