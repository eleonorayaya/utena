package workspacelist

import (
	"github.com/eleonorayaya/utena/internal/tui/uitest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ws(id uint, name string, ago time.Duration, opts ...func(*workspace.Workspace)) workspace.Workspace {
	w := workspace.Workspace{Name: name, Path: "/srv/" + name, IsGitRepo: true, LastUsedAt: time.Now().Add(-ago)}
	w.ID = id
	for _, o := range opts {
		o(&w)
	}
	return w
}

func bare(w *workspace.Workspace)   { w.IsBare = true }
func hidden(w *workspace.Workspace) { w.IsHidden = true }
func notGit(w *workspace.Workspace) { w.IsGitRepo = false }
func repo(name string) func(*workspace.Workspace) {
	return func(w *workspace.Workspace) { w.Repo = &git.Repo{FullName: name} }
}

func listOf(t *testing.T, w, h int, wss ...workspace.Workspace) Model {
	t.Helper()
	m, _ := New().Init()
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m, _ = m.Update(provider.WorkspacesStateUpdatedMsg{Workspaces: wss})
	return m
}

func cmdType(cmd tea.Cmd) string {
	types := uitest.MsgTypes(cmd)
	if len(types) == 0 {
		return ""
	}
	return types[0]
}

func TestWorkspaceList_SectionsColumnsAndPath(t *testing.T) {
	m := listOf(t, 70, 12,
		ws(1, "old", 72*time.Hour, bare, repo("acme/old")),
		ws(2, "utena", time.Hour, bare, repo("eleonorayaya/utena")),
		ws(3, "scratch", 3*time.Hour, notGit),
		ws(4, "proto", time.Hour, hidden, bare),
	)
	lines := strings.Split(m.View(), "\n")
	require.Len(t, lines, 12)
	assert.Contains(t, lines[0], "Workspaces 3")
	assert.True(t, strings.HasPrefix(lines[1], "▌ utena"))
	assert.Contains(t, lines[1], "eleonorayaya/utena")
	assert.Contains(t, lines[1], "bare")
	assert.True(t, strings.HasSuffix(strings.TrimRight(lines[1], " "), "1h"))
	assert.Contains(t, lines[2], "scratch")
	assert.Contains(t, lines[2], "—")
	assert.Contains(t, lines[2], "not git")
	assert.Contains(t, lines[3], "old")
	assert.NotContains(t, m.View(), "proto")
	assert.Equal(t, "  /srv/utena", strings.TrimRight(lines[11], " "))
	for _, l := range lines {
		assert.LessOrEqual(t, lipgloss.Width(l), 70)
	}

	m, _ = m.Update(uitest.Runes("."))
	out := m.View()
	assert.Contains(t, out, "Hidden 1")
	assert.Contains(t, out, "proto")
}

func TestWorkspaceList_CloningStatusReplacesKind(t *testing.T) {
	m := listOf(t, 70, 8, ws(1, "web", time.Hour, func(w *workspace.Workspace) { w.Status = workspace.StatusCloning }))
	assert.Contains(t, m.View(), "◌ cloning…")
}

func TestWorkspaceList_DeleteNeedsTwoPresses(t *testing.T) {
	m := listOf(t, 70, 8, ws(1, "web", time.Hour))
	m, cmd := m.Update(uitest.Runes("d"))
	assert.Nil(t, cmd)
	assert.Equal(t, "press d again to delete web", m.Status().Text)
	_, cmd = m.Update(uitest.Runes("d"))
	assert.Equal(t, "provider.deleteWorkspaceIntentMsg", cmdType(cmd))
}

func TestWorkspaceList_MigrateNeedsTwoPressesAndGuards(t *testing.T) {
	m := listOf(t, 70, 8, ws(1, "web", time.Hour))
	m, cmd := m.Update(uitest.Runes("m"))
	assert.Nil(t, cmd)
	assert.Equal(t, "press m again to migrate web to bare", m.Status().Text)
	_, cmd = m.Update(uitest.Runes("m"))
	assert.Equal(t, "provider.migrateWorkspaceToBareIntentMsg", cmdType(cmd))

	m = listOf(t, 70, 8, ws(1, "web", time.Hour, bare))
	m, cmd = m.Update(uitest.Runes("m"))
	assert.Nil(t, cmd)
	assert.Equal(t, "web is already bare", m.Status().Text)

	m = listOf(t, 70, 8, ws(1, "notes", time.Hour, notGit))
	m, _ = m.Update(uitest.Runes("m"))
	assert.Equal(t, "notes is not a git repository", m.Status().Text)
}

func TestWorkspaceList_OtherKeyCancelsPendingDelete(t *testing.T) {
	m := listOf(t, 70, 8, ws(1, "web", time.Hour))
	m, _ = m.Update(uitest.Runes("d"))
	m, _ = m.Update(uitest.Runes("m"))
	_, cmd := m.Update(uitest.Runes("d"))
	assert.Nil(t, cmd)
}

func TestWorkspaceList_FilterTypingDoesNotTriggerActions(t *testing.T) {
	m := listOf(t, 70, 8, ws(1, "web", time.Hour), ws(2, "data", 2*time.Hour))
	m, _ = m.Update(uitest.Runes("/"))
	assert.True(t, m.CapturesText())
	m, cmd := m.Update(uitest.Runes("d"))
	assert.NotEqual(t, "provider.deleteWorkspaceIntentMsg", cmdType(cmd))
	assert.True(t, m.Status().Empty())
}

func TestWorkspaceList_EmptyState(t *testing.T) {
	m := listOf(t, 70, 8)
	assert.Contains(t, m.View(), "No workspaces — press c to clone one")
}

func TestWorkspaceList_ShortHelp(t *testing.T) {
	var got []string
	for _, b := range keys.ShortHelp() {
		got = append(got, b.Help().Key+" "+b.Help().Desc)
	}
	assert.Equal(t, []string{"c clone", "m migrate", "h hide", "d delete"}, got)
}

func TestWorkspaceList_RefreshWhileFilteredKeepsMatches(t *testing.T) {
	wss := []workspace.Workspace{ws(1, "web", time.Hour), ws(2, "data", 2*time.Hour), ws(3, "webhooks", 3*time.Hour)}
	m := listOf(t, 70, 10, wss...)
	m, _ = m.Update(uitest.Runes("/"))
	m = typeFilter(m, "hooks")
	m, _ = m.Update(provider.WorkspacesStateUpdatedMsg{Workspaces: wss})
	out := m.View()
	assert.Contains(t, out, "webhooks")
	assert.NotContains(t, out, "No items")
	assert.Contains(t, out, "/srv/webhooks")
}

func typeFilter(m Model, s string) Model {
	for _, r := range s {
		var cmd tea.Cmd
		m, cmd = m.Update(uitest.Runes(string(r)))
		for _, msg := range uitest.Drain(cmd) {
			if fm, ok := msg.(list.FilterMatchesMsg); ok {
				m, _ = m.Update(fm)
			}
		}
	}
	return m
}
