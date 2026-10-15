package sessionlist

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/stretchr/testify/assert"
)

func TestRenderRow_Columns(t *testing.T) {
	s := withClaude(1, "checkout-flow-redesign", claude.StatusReadyForReview)
	s.IsAttached = true
	s.LastUsedAt = time.Now().Add(-45 * time.Minute)
	s.PullRequests = []git.PullRequest{{State: git.PRStateOpen}, {State: git.PRStateDraft}, {State: git.PRStateMerged}}

	out := New().renderRow(s, false, 50)
	assert.Equal(t, 50, lipgloss.Width(out))
	assert.True(t, strings.HasPrefix(out, "  ✓ checkout-flow-redesign"))
	assert.Contains(t, out, "◆")
	assert.Contains(t, out, "●1 ◌1")
	assert.True(t, strings.HasSuffix(out, " 45m  "))
}

func TestRenderRow_SelectedHasGutterBar(t *testing.T) {
	out := New().renderRow(withClaude(1, "x", claude.StatusIdle), true, 40)
	assert.True(t, strings.HasPrefix(out, "▌ "))
	assert.Equal(t, 40, lipgloss.Width(out))
}

func TestRenderRow_LongAndWideNamesFitWidth(t *testing.T) {
	for _, name := range []string{strings.Repeat("very-long-session-name-", 6), "日本語のセッション名がとても長い場合", "🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀"} {
		for _, w := range []int{20, 36, 52} {
			out := New().renderRow(withClaude(1, name, claude.StatusWorking), true, w)
			assert.LessOrEqual(t, lipgloss.Width(out), w, "%q at %d", name, w)
		}
	}
}

func TestSessionList_ShortHelp(t *testing.T) {
	var got []string
	for _, b := range keys.ShortHelp() {
		got = append(got, b.Help().Key+" "+b.Help().Desc)
	}
	assert.Equal(t, []string{"enter switch", "o open PR", "n new", "b block", "a archive"}, got)
}

func TestBrowserSpan_States(t *testing.T) {
	open := browserSpan("open")
	assert.Equal(t, "◫", open.Text)
	assert.Equal(t, theme.Current.AccentBlue, open.Style.GetForeground())
	for _, st := range []string{"missing", "snapshotted"} {
		sp := browserSpan(st)
		assert.Equal(t, "◫", sp.Text, st)
		assert.Equal(t, theme.Current.TextMuted, sp.Style.GetForeground(), st)
	}
	assert.Equal(t, "", browserSpan("").Text)
}

func TestRenderRow_BrowserMarker(t *testing.T) {
	s := withClaude(7, "web", claude.StatusIdle)
	s.IsAttached = true
	m := New()
	m, _ = m.Update(provider.BrowserStateUpdatedMsg{State: browser.SessionsView{Sessions: map[uint]browser.SessionBrowser{7: {State: "open", TabCount: 2}}}})
	out := ansi.Strip(m.renderRow(s, false, 40))
	assert.Equal(t, 40, lipgloss.Width(out))
	assert.Equal(t, "  · web"+strings.Repeat(" ", 16)+"◆ ◫ "+strings.Repeat(" ", 13), out)

	without := ansi.Strip(New().renderRow(s, false, 40))
	assert.Equal(t, "  · web"+strings.Repeat(" ", 16)+"◆   "+strings.Repeat(" ", 13), without)
}

func TestRenderRow_BrowserMarkerOnlyForOwnSession(t *testing.T) {
	m := New()
	m, _ = m.Update(provider.BrowserStateUpdatedMsg{State: browser.SessionsView{Sessions: map[uint]browser.SessionBrowser{8: {State: "open"}}}})
	assert.NotContains(t, m.renderRow(withClaude(7, "web", claude.StatusIdle), false, 40), "◫")
}
