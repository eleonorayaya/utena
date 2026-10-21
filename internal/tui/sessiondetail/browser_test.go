package sessiondetail

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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
	m = selectIn(m, s).Focus()
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
	m := detailWith(makeActiveSession(), oneWindow()).Blur()
	got := browserLines(m.View())
	assert.Equal(t, []string{
		"  Browser " + strings.Repeat("─", 34) + " 2 tabs · open",
		"    ● Pull request #42  github.com",
		"      Docs  docs.example.com",
	}, got)
	for _, l := range strings.Split(m.View(), "\n") {
		assert.LessOrEqual(t, lipgloss.Width(l), 60)
	}
}

func TestBrowserSection_SeveralWindowsAndSnapshotted(t *testing.T) {
	m := detailWith(makeActiveSession(), twoWindows()).Blur()
	assert.Equal(t, []string{
		"  Browser " + strings.Repeat("─", 34) + " 3 tabs · open",
		"    window 1",
		"    ● Pull request #42  github.com",
		"    window 2 · snapshotted",
		"    ● Old notes  notes.example.com",
		"      https://blank.example.com/b",
	}, browserLines(m.View()))
}

func TestBrowserSection_SnapshottedOnlyIsMutedAndNotSelectable(t *testing.T) {
	sb := browser.SessionBrowser{State: "snapshotted", TabCount: 1, Windows: []browser.WindowView{
		{State: "snapshotted", Tabs: []browser.TabView{{Title: "Saved", URL: "https://saved.example.com", Active: true}}},
	}}
	m := detailWith(makeActiveSession(), sb)
	assert.Equal(t, []string{
		"  Browser " + strings.Repeat("─", 28) + " 1 tab · snapshotted",
		"    ● Saved  saved.example.com",
	}, browserLines(m.View()))
	m, _ = m.Update(runeKey("j"))
	assert.Equal(t, -1, m.cursor)
	assert.Equal(t, "switch", m.Keys().ShortHelp()[0].Help().Desc)
}

func TestBrowserSection_PendingTabsAreMarkedAndNotSelectable(t *testing.T) {
	sb := browser.SessionBrowser{State: "open", TabCount: 2, Windows: []browser.WindowView{
		{ID: "w1", State: "open", Tabs: []browser.TabView{
			{ID: "t1", Title: "Docs", URL: "https://docs.example.com", Active: true, State: browser.TabOpen},
			{URL: "https://github.com/o/r/pull/7", State: browser.TabPending},
		}},
	}}
	m := detailWith(makeActiveSession(), sb)
	assert.Equal(t, []string{
		"  Browser " + strings.Repeat("─", 34) + " 2 tabs · open",
		"  ▌ ● Docs  docs.example.com",
		"    ◌ https://github.com/o/r/pull/7",
	}, browserLines(m.View()))
	assert.Len(t, m.selectableTabs(), 1)
}

func TestBrowserSection_OmittedWithoutRows(t *testing.T) {
	m := focusedOn(makeActiveSession())
	assert.NotContains(t, m.View(), "Browser")
	m, _ = m.Update(browserState(999, oneWindow()))
	assert.NotContains(t, m.View(), "Browser")
}

func TestBrowserSection_PanelPreviewHasNoCursor(t *testing.T) {
	out := detailWith(makeActiveSession(), oneWindow()).Blur().View()
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
	assert.Equal(t, provider.FocusTabIntentMsg{SessionID: 1, WindowID: "w1", TabID: "t2"}, openTarget(t, m))

	m, _ = m.Update(runeKey("j"))
	assert.Equal(t, provider.FocusTabIntentMsg{SessionID: 1, WindowID: "w1", TabID: "t2"}, openTarget(t, m))
}

func TestBrowserCursor_SkipsUnfocusableTabs(t *testing.T) {
	m := detailWith(makeActiveSession(), twoWindows())
	m, _ = m.Update(runeKey("j"))
	m, _ = m.Update(runeKey("j"))
	assert.Equal(t, provider.FocusTabIntentMsg{SessionID: 1, WindowID: "w1", TabID: "t1"}, openTarget(t, m))
}

func TestBrowserCursor_EnterOnTabOpensIt(t *testing.T) {
	attached := makeActiveSession()
	attached.IsAttached = true
	m := detailWith(attached, oneWindow())
	assert.Equal(t, 0, m.cursor)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, provider.FocusTabIntentMsg{SessionID: 1, WindowID: "w1", TabID: "t1"}, cmd())
}

func TestBrowserCursor_EnterOnBlockerSwitches(t *testing.T) {
	m := detailWith(makeBlockedSession(), oneWindow())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, "provider.activateSessionMsg", msgType(cmd()))
}

func TestBrowserCursor_UpStopsAtFirstItem(t *testing.T) {
	m := detailWith(makeActiveSession(), oneWindow())
	m, _ = m.Update(runeKey("j"))
	assert.Equal(t, 1, m.cursor)
	m, _ = m.Update(runeKey("k"))
	m, _ = m.Update(runeKey("k"))
	assert.Equal(t, 0, m.cursor)
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
	m = selectIn(m, refreshed)
	assert.Equal(t, 1, m.cursor)
}

func oneBlocker() session.Session {
	s := makeActiveSession()
	s.Blockers = []session.SessionBlocker{{Model: gorm.Model{ID: 10}, Kind: session.BlockerKindReview, Note: "only"}}
	return s
}

func openTarget(t *testing.T, m Model) tea.Msg {
	t.Helper()
	_, cmd := m.Update(runeKey("o"))
	require.NotNil(t, cmd)
	return cmd()
}

func TestBrowserCursor_StaysOnTabWhenBlockersChange(t *testing.T) {
	m := detailWith(oneBlocker(), oneWindow())
	m, _ = m.Update(runeKey("j"))
	want := provider.FocusTabIntentMsg{SessionID: 1, WindowID: "w1", TabID: "t1"}
	assert.Equal(t, want, openTarget(t, m))

	resolved := oneBlocker()
	now := time.Now()
	resolved.Blockers[0].ResolvedAt = &now
	m = selectIn(m, resolved)
	assert.Equal(t, want, openTarget(t, m))

	m = selectIn(m, makeBlockedSession())
	assert.Equal(t, want, openTarget(t, m))
}

func TestBrowserCursor_StaysOnTabWhenTabsChange(t *testing.T) {
	m := detailWith(makeActiveSession(), oneWindow())
	grown := oneWindow()
	grown.Windows[0].Tabs = append([]browser.TabView{{ID: "t0", Title: "New", URL: "https://new.example.com"}}, grown.Windows[0].Tabs...)
	m, _ = m.Update(browserState(1, grown))
	assert.Equal(t, provider.FocusTabIntentMsg{SessionID: 1, WindowID: "w1", TabID: "t1"}, openTarget(t, m))
}

func TestBrowserCursor_BlockerGoneClampsWithinBlockers(t *testing.T) {
	m := detailWith(makeBlockedSession(), oneWindow())
	m, _ = m.Update(runeKey("j"))
	now := time.Now()
	refreshed := makeBlockedSession()
	refreshed.Blockers[1].ResolvedAt = &now
	m = selectIn(m, refreshed)
	assert.Equal(t, 0, m.cursor)

	gone := makeBlockedSession()
	gone.Blockers[0].ResolvedAt = &now
	gone.Blockers[1].ResolvedAt = &now
	m = selectIn(m, gone)
	assert.Equal(t, 0, m.cursor)
	assert.Equal(t, provider.FocusTabIntentMsg{SessionID: 1, WindowID: "w1", TabID: "t1"}, openTarget(t, m))
}

func TestBrowserSection_SanitizesControlCharacters(t *testing.T) {
	sb := oneWindow()
	sb.Windows[0].Tabs[0].Title = "evil\x1b]52;c;Zm9v\atitle"
	m := detailWith(makeActiveSession(), sb)
	out := m.View()
	assert.NotContains(t, out, "\x1b]52")
	assert.NotContains(t, out, "\a")
	assert.Contains(t, ansi.Strip(out), "eviltitle")
}

func TestBrowserSection_UntitledTabShowsURLOnly(t *testing.T) {
	sb := oneWindow()
	sb.Windows[0].Tabs[1].Title = ""
	m := detailWith(makeActiveSession(), sb)
	assert.Equal(t, "      https://docs.example.com/guide", lineWith(plainLines(m.View()), "docs.example.com"))
}

func TestBrowserSection_UnfocusableOpenTabIsMuted(t *testing.T) {
	sb := oneWindow()
	sb.Windows[0].Tabs[1].ID = ""
	live := tabLine(sb.Windows[0], sb.Windows[0].Tabs[0], false, 40)
	stale := tabLine(sb.Windows[0], sb.Windows[0].Tabs[1], false, 40)
	assert.Contains(t, live, valueStyle().Render("Pull request #42"))
	assert.Contains(t, stale, mutedStyle().Render("Docs"))
}

func TestBrowserSection_FitsNarrowAndWideRunes(t *testing.T) {
	sb := oneWindow()
	sb.Windows[0].Tabs[0].Title = "日本語のタイトルがとても長い場合のテスト"
	sb.Windows[0].Tabs[1].Title = "🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀"
	for _, w := range []int{20, 60} {
		m, _ := New().Update(tea.WindowSizeMsg{Width: w, Height: 40})
		m = selectIn(m, makeActiveSession()).Focus()
		m, _ = m.Update(browserState(1, sb))
		m, _ = m.Update(runeKey("j"))
		out := m.View()
		assert.Contains(t, out, "▌")
		for _, l := range strings.Split(out, "\n") {
			assert.LessOrEqual(t, lipgloss.Width(l), w, "%q", ansi.Strip(l))
		}
	}
}

func TestDetail_ShortHelpShowsNextWhenTabsSelectable(t *testing.T) {
	m := detailWith(makeActiveSession(), oneWindow())
	var got []string
	for _, b := range m.Keys().ShortHelp() {
		got = append(got, b.Help().Key+" "+b.Help().Desc)
	}
	assert.Equal(t, []string{"enter/o open tab", "↓/j next item", "b block", "a archive", "tab/esc sessions"}, got)

	m = focusedOn(makeActiveSession())
	assert.Len(t, m.Keys().ShortHelp(), 4)
}

func threeTabs() browser.SessionBrowser {
	sb := oneWindow()
	sb.TabCount = 3
	sb.Windows[0].Tabs = append(sb.Windows[0].Tabs, browser.TabView{ID: "t3", Title: "Third", URL: "https://third.example.com"})
	return sb
}

func TestBrowserCursor_TabGoneWhileBlockersChangeKeepsTabPosition(t *testing.T) {
	m := detailWith(makeBlockedSession(), threeTabs())
	m, _ = m.Update(runeKey("j"))
	m, _ = m.Update(runeKey("j"))
	sel := m.currentSelection()
	require.Equal(t, "t1", sel.item.tab.tabID)

	fewer := makeBlockedSession()
	fewer.Blockers = fewer.Blockers[:1]
	m.sess = &fewer
	gone := threeTabs()
	gone.Windows[0].Tabs = gone.Windows[0].Tabs[1:]
	m.browserState = browserState(1, gone).State
	m.restoreSelection(sel)
	assert.Equal(t, provider.FocusTabIntentMsg{SessionID: 1, WindowID: "w1", TabID: "t2"}, openTarget(t, m))

	m.browserState = browserState(1, browser.SessionBrowser{}).State
	m.restoreSelection(sel)
	assert.Equal(t, 0, m.cursor)
}
