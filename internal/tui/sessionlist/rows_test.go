package sessionlist

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/git"
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
