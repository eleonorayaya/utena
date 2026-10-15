package sessiondetail

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func browserState(id uint, sb browser.SessionBrowser) provider.BrowserStateUpdatedMsg {
	return provider.BrowserStateUpdatedMsg{State: browser.SessionsView{Connected: true, Sessions: map[uint]browser.SessionBrowser{id: sb}}}
}

func msgType(msg tea.Msg) string {
	return reflect.TypeOf(msg).String()
}

func oneWindow() browser.SessionBrowser {
	return browser.SessionBrowser{State: "open", TabCount: 2, Windows: []browser.WindowView{
		{ID: "w1", State: "open", Focused: true, Tabs: []browser.TabView{
			{ID: "t1", Title: "Pull request #42", URL: "https://www.github.com/o/r/pull/42", Active: true},
			{ID: "t2", Title: "Docs", URL: "https://docs.example.com/guide"},
		}},
	}}
}

func twoWindows() browser.SessionBrowser {
	return browser.SessionBrowser{State: "open", TabCount: 3, Windows: []browser.WindowView{
		{ID: "w1", State: "open", Tabs: []browser.TabView{
			{ID: "t1", Title: "Pull request #42", URL: "https://github.com/o/r/pull/42", Active: true},
		}},
		{ID: "", State: "snapshotted", Tabs: []browser.TabView{
			{Title: "Old notes", URL: "https://notes.example.com/a", Active: true},
			{Title: "", URL: "https://blank.example.com/b"},
		}},
	}}
}

func detailWith(s session.Session, sb browser.SessionBrowser) Model {
	m, _ := New().Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m, _ = m.Update(SelectMsg{Session: s})
	m, _ = m.Update(browserState(s.ID, sb))
	return m
}

func browserLines(out string) []string {
	lines := plainLines(out)
	for i, l := range lines {
		if strings.Contains(l, "Browser ─") {
			end := i + 1
			for end < len(lines) && lines[end] != "" {
				end++
			}
			return lines[i:end]
		}
	}
	return nil
}

func TestBrowserSection_OneWindow(t *testing.T) {
	m := detailWith(makeActiveSession(), oneWindow())
	got := browserLines(m.View())
	assert.Equal(t, []string{
		"  Browser "+strings.Repeat("─", 34)+" 2 tabs · open",
		"    ● Pull request #42  github.com",
		"      Docs  docs.example.com",
	}, got)
	for _, l := range strings.Split(m.View(), "\n") {
		assert.LessOrEqual(t, lipgloss.Width(l), 60)
	}
}

func TestBrowserSection_SeveralWindowsAndSnapshotted(t *testing.T) {
	m := detailWith(makeActiveSession(), twoWindows())
	assert.Equal(t, []string{
		"  Browser "+strings.Repeat("─", 34)+" 3 tabs · open",
		"    window 1",
		"    ● Pull request #42  github.com",
		"    window 2 · snapshotted",
		"    ● Old notes  notes.example.com",
		"      https://blank.example.com/b  blank.example.com",
	}, browserLines(m.View()))
}

func TestBrowserSection_SnapshottedOnlyIsMutedAndNotSelectable(t *testing.T) {
	sb := browser.SessionBrowser{State: "snapshotted", TabCount: 1, Windows: []browser.WindowView{
		{State: "snapshotted", Tabs: []browser.TabView{{Title: "Saved", URL: "https://saved.example.com", Active: true}}},
	}}
	m := detailWith(makeActiveSession(), sb)
	assert.Equal(t, []string{
		"  Browser "+strings.Repeat("─", 28)+" 1 tab · snapshotted",
		"    ● Saved  saved.example.com",
	}, browserLines(m.View()))
	m, _ = m.Update(runeKey("j"))
	assert.Equal(t, -1, m.cursor)
	assert.Equal(t, "switch", m.Keys().ShortHelp()[0].Help().Desc)
}

func TestBrowserSection_OmittedWithoutRows(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeActiveSession()})
	assert.NotContains(t, m.View(), "Browser")
	m, _ = m.Update(browserState(999, oneWindow()))
	assert.NotContains(t, m.View(), "Browser")
}

func TestBrowserSection_PanelPreviewHasNoCursor(t *testing.T) {
	s := makeActiveSession()
	out := RenderPanelWithBrowser(&s, browser.SessionsView{Sessions: map[uint]browser.SessionBrowser{s.ID: oneWindow()}}, 60, 30)
	assert.Contains(t, out, "Pull request #42")
	assert.NotContains(t, out, "▌")
}

func TestBrowserCursor_MovesFromBlockersIntoTabs(t *testing.T) {
	m := detailWith(makeBlockedSession(), oneWindow())
	assert.Equal(t, 0, m.cursor)
	m, _ = m.Update(runeKey("j"))
	m, _ = m.Update(runeKey("j"))
	assert.Contains(t, lineWith(plainLines(m.View()), "Pull request"), "▌ ● Pull request #42")
	assert.Equal(t, "open tab", m.Keys().ShortHelp()[0].Help().Desc)

	_, cmd := m.Update(runeKey("u"))
	assert.Nil(t, cmd)
	_, cmd = m.Update(runeKey("x"))
	assert.Nil(t, cmd)

	m, _ = m.Update(runeKey("j"))
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, provider.FocusTabIntentMsg{SessionID: 1, WindowID: "w1", TabID: "t2"}, cmd())

	m, _ = m.Update(runeKey("j"))
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, provider.FocusTabIntentMsg{SessionID: 1, WindowID: "w1", TabID: "t2"}, cmd())
}

func TestBrowserCursor_SkipsUnfocusableTabs(t *testing.T) {
	m := detailWith(makeActiveSession(), twoWindows())
	m, _ = m.Update(runeKey("j"))
	m, _ = m.Update(runeKey("j"))
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, provider.FocusTabIntentMsg{SessionID: 1, WindowID: "w1", TabID: "t1"}, cmd())
}

func TestBrowserCursor_EnterWithoutSelectionSwitches(t *testing.T) {
	m := detailWith(makeActiveSession(), oneWindow())
	assert.Equal(t, -1, m.cursor)
	assert.Equal(t, "switch", m.Keys().ShortHelp()[0].Help().Desc)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, "provider.activateSessionMsg", msgType(cmd()))
}

func TestBrowserCursor_EnterOnBlockerSwitches(t *testing.T) {
	m := detailWith(makeBlockedSession(), oneWindow())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, "provider.activateSessionMsg", msgType(cmd()))
}

func TestBrowserCursor_UpReturnsToNoSelection(t *testing.T) {
	m := detailWith(makeActiveSession(), oneWindow())
	m, _ = m.Update(runeKey("j"))
	assert.Equal(t, 0, m.cursor)
	m, _ = m.Update(runeKey("k"))
	m, _ = m.Update(runeKey("k"))
	assert.Equal(t, -1, m.cursor)
}

func TestBrowserCursor_ClampsWhenStateChanges(t *testing.T) {
	m := detailWith(makeActiveSession(), oneWindow())
	m, _ = m.Update(runeKey("j"))
	m, _ = m.Update(runeKey("j"))
	assert.Equal(t, 1, m.cursor)

	shrunk := oneWindow()
	shrunk.Windows[0].Tabs = shrunk.Windows[0].Tabs[:1]
	m, _ = m.Update(browserState(1, shrunk))
	assert.Equal(t, 0, m.cursor)

	m, _ = m.Update(browserState(1, browser.SessionBrowser{}))
	assert.Equal(t, -1, m.cursor)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, "provider.activateSessionMsg", msgType(cmd()))
}

func TestBrowserCursor_ClampsWhenBlockersResolve(t *testing.T) {
	m := detailWith(makeBlockedSession(), oneWindow())
	m, _ = m.Update(runeKey("j"))
	m, _ = m.Update(runeKey("j"))
	m, _ = m.Update(runeKey("j"))
	assert.Equal(t, 3, m.cursor)

	refreshed := makeBlockedSession()
	refreshed.Blockers = nil
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{refreshed}})
	assert.Equal(t, 1, m.cursor)
}

func TestDetail_SelectRequestsBrowserState(t *testing.T) {
	_, cmd := New().Update(SelectMsg{Session: makeActiveSession()})
	require.NotNil(t, cmd)
	assert.Equal(t, "provider.requestBrowserStateMsg", msgType(cmd()))
}

func TestDetail_ErrorShowsInFooter(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeActiveSession()})
	m, _ = m.Update(provider.ErrMsg{Err: errors.New("browser unavailable")})
	assert.Equal(t, ui.Status{Text: "browser unavailable", Kind: ui.StatusError}, m.Status())
}
