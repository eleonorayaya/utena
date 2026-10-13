package sessiondetail

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func richSession() session.Session {
	return session.Session{
		Model:          gorm.Model{ID: 1},
		Name:           "checkout-flow-redesign",
		Status:         session.StatusActive,
		IsAttached:     true,
		LastUsedAt:     time.Now(),
		SessionRoot:    "/Users/x/utena-sessions/checkout-flow-redesign",
		ClaudeSessions: []claude.ClaudeSession{{Status: claude.StatusReadyForReview}},
		Worktrees:      []session.SessionWorktree{wt(0, "api-service", 1, 10), wt(1, "data-sync", 3, 30)},
		PullRequests: []git.PullRequest{
			{Number: 4847, RepoID: 1, Title: "fix(ABC-123): stop scheduled jobs from running twice per window", State: git.PRStateDraft, HTMLURL: "https://github.com/o/api-service/pull/4847"},
		},
	}
}

func TestPanel_HeaderAndStatusLine(t *testing.T) {
	s := richSession()
	out := RenderPanel(&s, 80, 20)
	lines := strings.Split(out, "\n")
	assert.True(t, strings.HasPrefix(lines[0], "checkout-flow-redesign"))
	assert.True(t, strings.HasSuffix(strings.TrimRight(lines[0], " "), "◆ attached"))
	assert.Equal(t, "✓ Ready for review · used just now", strings.TrimRight(lines[1], " "))
	assert.NotContains(t, lines[0], "active")
}

func TestPanel_PillOnlyWhenNotActive(t *testing.T) {
	s := richSession()
	s.Status = session.StatusBroken
	out := RenderPanel(&s, 80, 20)
	assert.Contains(t, strings.Split(out, "\n")[0], "broken")
}

func TestPanel_WorkspacesAndPRsWithLinks(t *testing.T) {
	s := richSession()
	out := RenderPanel(&s, 80, 20)
	assert.Contains(t, out, "api-service")
	assert.Contains(t, out, "\x1b]8;;https://github.com/o/api-service/pull/4847\x1b\\")
	assert.Contains(t, out, "◌ draft")
	assert.Contains(t, out, "data-sync")
	assert.Contains(t, out, "no pull requests")
}

func TestPanel_PRWithoutURLHasNoLinkEscape(t *testing.T) {
	s := richSession()
	s.PullRequests[0].HTMLURL = ""
	out := RenderPanel(&s, 80, 20)
	assert.Contains(t, out, "#4847")
	assert.NotContains(t, out, "\x1b]8;;")
}

func TestPanel_RemovedSections(t *testing.T) {
	s := richSession()
	out := RenderPanel(&s, 80, 30)
	for _, gone := range []string{"Tmux", "Root", "Past", "◆ Claude", "utena-sessions/"} {
		assert.NotContains(t, out, gone)
	}
}

func TestPanel_LinesFitWidthAndHeight(t *testing.T) {
	s := richSession()
	s.Name = strings.Repeat("日本語", 30)
	s.Status = session.StatusCreating
	s.StatusError = strings.Repeat("something went badly wrong ", 10)
	for _, w := range []int{30, 60, 120} {
		out := RenderPanel(&s, w, 12)
		lines := strings.Split(out, "\n")
		assert.Len(t, lines, 12)
		for _, l := range lines {
			assert.LessOrEqual(t, lipgloss.Width(l), w, "width %d: %q", w, l)
		}
	}
}

func TestPanel_EmptyState(t *testing.T) {
	assert.Contains(t, RenderPanel(nil, 60, 10), "Select a session")
}

func TestPanel_ProblemCallout(t *testing.T) {
	s := richSession()
	s.SessionActions = []session.SessionAction{{Type: "pull", Error: "dirty tree"}}
	out := RenderPanel(&s, 80, 20)
	assert.Contains(t, out, "⚠ action pull failed: dirty tree")
}
