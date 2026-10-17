package sessiondetail

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

func plainLines(out string) []string {
	lines := strings.Split(ansi.Strip(out), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return lines
}

func lineWith(lines []string, sub string) string {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return l
		}
	}
	return ""
}

func TestPanel_HeaderBandSpansFullWidth(t *testing.T) {
	s := richSession()
	out := RenderPanel(&s, 80, 20)
	band := strings.Split(out, "\n")[0]
	assert.Equal(t, 80, lipgloss.Width(band))
	plain := ansi.Strip(band)
	assert.True(t, strings.HasPrefix(plain, "  checkout-flow-redesign"), plain)
	assert.True(t, strings.HasSuffix(plain, "◆ attached  "), plain)
	assert.NotContains(t, plain, "active")
}

func TestPanel_StatusLineShowsCreatedAge(t *testing.T) {
	s := richSession()
	assert.Equal(t, "  ✓ Ready for review · used just now", plainLines(RenderPanel(&s, 80, 20))[1])
	s.CreatedAt = time.Now().Add(-49 * time.Hour)
	assert.Equal(t, "  ✓ Ready for review · used just now · created 2d ago", plainLines(RenderPanel(&s, 80, 20))[1])
}

func TestPanel_WorkspacesSectionListsBranchesWithoutPaths(t *testing.T) {
	s := richSession()
	s.Worktrees[0].Worktree.Path = "/Users/x/utena-sessions/checkout-flow-redesign/api-service"
	s.Worktrees[0].Worktree.Branch = &git.Branch{Name: "feat/scheduler"}
	s.Worktrees[1].Worktree.Branch = &git.Branch{Name: "feat/sync"}
	lines := plainLines(RenderPanel(&s, 80, 30))
	header := lineWith(lines, "Workspaces")
	assert.True(t, strings.HasPrefix(header, "  Workspaces ─"), header)
	assert.True(t, strings.HasSuffix(header, "─ 2"), header)
	assert.Equal(t, 78, lipgloss.Width(header))
	assert.Regexp(t, `^    api-service +feat/scheduler$`, lineWith(lines, "feat/scheduler"))
	assert.Regexp(t, `^    data-sync +feat/sync$`, lineWith(lines, "feat/sync"))
	assert.Equal(t, strings.Index(lineWith(lines, "feat/scheduler"), "feat/"), strings.Index(lineWith(lines, "feat/sync"), "feat/"))
	assert.NotContains(t, strings.Join(lines, "\n"), "utena-sessions/")
}

func TestPanel_PRGroupsUseRulesWithSummaries(t *testing.T) {
	s := richSession()
	s.PullRequests = append(s.PullRequests,
		git.PullRequest{Number: 4800, RepoID: 1, Title: "older work", State: git.PRStateMerged},
		git.PullRequest{Number: 4850, RepoID: 1, Title: "newer work", State: git.PRStateOpen},
	)
	lines := plainLines(RenderPanel(&s, 80, 30))
	apiRule := lineWith(lines, "api-service ─")
	assert.True(t, strings.HasPrefix(apiRule, "  api-service ─"), apiRule)
	assert.True(t, strings.HasSuffix(apiRule, "─ 1 open · 1 draft · 1 merged"), apiRule)
	syncRule := lineWith(lines, "data-sync ─")
	assert.True(t, strings.HasSuffix(syncRule, "─ no pull requests"), syncRule)
	assert.Regexp(t, `^    #4850 +newer work +● open$`, lineWith(lines, "#4850"))
}

func TestPanel_PRRowsShowChecks(t *testing.T) {
	s := richSession()
	s.PullRequests = []git.PullRequest{
		{Number: 1, RepoID: 1, Title: "passing", State: git.PRStateOpen, ChecksState: git.ChecksStatePassed},
		{Number: 2, RepoID: 1, Title: "failing", State: git.PRStateOpen, ChecksState: git.ChecksStateFailing},
		{Number: 3, RepoID: 1, Title: "running", State: git.PRStateOpen, ChecksState: git.ChecksStatePending},
		{Number: 4, RepoID: 1, Title: "unknown", State: git.PRStateOpen},
	}
	lines := plainLines(RenderPanel(&s, 80, 30))
	assert.Regexp(t, `passing +✓  ● open$`, lineWith(lines, "passing"))
	assert.Regexp(t, `failing +✕  ● open$`, lineWith(lines, "failing"))
	assert.Regexp(t, `running +◌  ● open$`, lineWith(lines, "running"))
	assert.Regexp(t, `unknown +● open$`, lineWith(lines, "unknown"))
	assert.Equal(t, len([]rune(lineWith(lines, "passing"))), len([]rune(lineWith(lines, "unknown"))))
}

func TestPanel_BlockersRenderInCard(t *testing.T) {
	s := richSession()
	s.Blockers = []session.SessionBlocker{{Kind: session.BlockerKindReview, Note: "waiting on review"}}
	lines := plainLines(RenderPanel(&s, 80, 30))
	top := lineWith(lines, "Blocked")
	assert.True(t, strings.HasPrefix(top, "  ╭─ Blocked 1 ─"), top)
	assert.True(t, strings.HasSuffix(top, "─╮"), top)
	body := lineWith(lines, "waiting on review")
	assert.True(t, strings.HasPrefix(body, "  │ "), body)
	assert.True(t, strings.HasSuffix(body, " │"), body)
	assert.NotEmpty(t, lineWith(lines, "╰"))
	assert.Equal(t, len([]rune(top)), len([]rune(body)))
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
	s.Blockers = []session.SessionBlocker{{Kind: session.BlockerKindReview, Note: strings.Repeat("long note ", 20)}}
	s.Worktrees[0].Worktree.Branch = &git.Branch{Name: strings.Repeat("very-long-branch-", 10)}
	s.PullRequests[0].ChecksState = git.ChecksStateFailed
	for _, w := range []int{8, 30, 60, 120} {
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
	lines := plainLines(RenderPanel(&s, 80, 20))
	assert.True(t, strings.HasPrefix(lineWith(lines, "Problem"), "  ╭─ Problem"), lineWith(lines, "Problem"))
	assert.True(t, strings.HasPrefix(lineWith(lines, "dirty tree"), "  │ ⚠ action pull failed: dirty tree"))
}

func TestRenderPanel_ShowsBundles(t *testing.T) {
	s := richSession()
	s.Bundles = []string{"go", "docs"}
	out := strings.Join(plainLines(RenderPanel(&s, 80, 0)), "\n")
	assert.Contains(t, out, "Bundles")
	assert.Contains(t, out, "go · docs")
	assert.NotContains(t, out, "unknown")
}

func TestRenderPanel_NoBundlesSection(t *testing.T) {
	s := richSession()
	out := strings.Join(plainLines(RenderPanel(&s, 80, 0)), "\n")
	assert.NotContains(t, out, "Bundles")
}
