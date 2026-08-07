package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func demoSidebar() sidebar {
	s1 := Session{Name: "eqt-checkout-flow", WorkspaceID: "w1M",
		Checkouts: []Checkout{
			{Label: "api", Branch: "eqt/checkout-flow", WorkspaceID: "w1H"},
			{Label: "web", Branch: "eqt/checkout-flow", WorkspaceID: "w1J"},
			{Label: "svc", Branch: "eqt/checkout-flow", WorkspaceID: "w1K"},
		}}
	s2 := Session{Name: "eqt-monitor-fix", WorkspaceID: "w20",
		Checkouts: []Checkout{{Label: "utena", Branch: "eqt/monitor-fix", WorkspaceID: "w21"}}}

	m := newSidebar(nil)
	m.width, m.height = 46, 14
	m.rows = []row{
		{kind: rowSession, session: &s1, status: "working"},
		{kind: rowCheckout, session: &s1, checkout: &s1.Checkouts[0], status: "working", branch: "eqt/checkout-flow"},
		{kind: rowCheckout, session: &s1, checkout: &s1.Checkouts[1], status: "idle", branch: "eqt/checkout-flow", dirty: 3},
		{kind: rowCheckout, session: &s1, checkout: &s1.Checkouts[2], status: "blocked", branch: "eqt/checkout-flow", last: true},
		{kind: rowSession, session: &s2, status: "done"},
		{kind: rowCheckout, session: &s2, checkout: &s2.Checkouts[0], status: "done", branch: "eqt/monitor-fix", last: true},
	}
	return m
}

func TestSidebarView(t *testing.T) {
	m := demoSidebar()
	out := m.View()
	t.Logf("rendered sidebar:\n%s", out)

	for _, want := range []string{"sessions", "eqt-checkout-flow", "api", "web", "svc", "└─", "├─", "●3"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q\n%s", want, out)
		}
	}
	if lines := strings.Count(out, "\n"); lines < 6 {
		t.Errorf("expected at least 6 rendered lines, got %d\n%s", lines, out)
	}
}

func TestSidebarNavigationClamps(t *testing.T) {
	m := demoSidebar()
	for i := 0; i < 20; i++ {
		next, _ := m.Update(keyPress("j"))
		m = next.(sidebar)
	}
	if m.cursor != len(m.rows)-1 {
		t.Errorf("cursor should clamp to last row %d, got %d", len(m.rows)-1, m.cursor)
	}
	for i := 0; i < 20; i++ {
		next, _ := m.Update(keyPress("k"))
		m = next.(sidebar)
	}
	if m.cursor != 0 {
		t.Errorf("cursor should clamp to 0, got %d", m.cursor)
	}
}

func TestArchiveRequiresTwoPresses(t *testing.T) {
	m := demoSidebar()
	next, _ := m.Update(keyPress("a"))
	m = next.(sidebar)
	if m.pending != "archive:eqt-checkout-flow" {
		t.Fatalf("first press should arm confirmation, got pending=%q", m.pending)
	}
	next, _ = m.Update(keyPress("j"))
	m = next.(sidebar)
	if m.pending != "" {
		t.Errorf("navigating should clear the pending confirmation, got %q", m.pending)
	}
}

func TestPickReposView(t *testing.T) {
	m := demoSidebar()
	m.mode = modePickRepos
	m.repos = []repoChoice{
		{path: "/home/e/workspace/api", selected: true},
		{path: "/home/e/workspace/web"},
		{path: "/home/e/workspace/svc", selected: true},
	}
	out := m.View()
	t.Logf("repo picker:\n%s", out)
	for _, want := range []string{"select repos", "api", "web", "svc", "2 selected", "space toggle"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}
}

func TestBranchView(t *testing.T) {
	m := demoSidebar()
	m.repos = []repoChoice{{path: "/home/e/workspace/api", selected: true}}
	m.mode = modeBranch
	m.branchInput = textinput.New()
	m.branchInput.Prompt = "branch: "
	m.branchInput.SetValue("eqt/my-feature")
	out := m.View()
	t.Logf("branch input:\n%s", out)
	for _, want := range []string{"new session", "api", "branch:", "eqt/my-feature", "enter create"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}
}

func TestSpaceTogglesRepoSelection(t *testing.T) {
	m := demoSidebar()
	m.mode = modePickRepos
	m.repos = []repoChoice{{path: "/a"}, {path: "/b"}}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	m = next.(sidebar)
	if len(m.selectedRepos()) != 1 || m.selectedRepos()[0] != "/a" {
		t.Errorf("space should select the cursor row, got %v", m.selectedRepos())
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	m = next.(sidebar)
	if len(m.selectedRepos()) != 0 {
		t.Errorf("space should deselect, got %v", m.selectedRepos())
	}
}

func TestBuildPickRows(t *testing.T) {
	sessions := []Session{
		{Name: "alpha", WorkspaceID: "w1", Checkouts: []Checkout{
			{Label: "api", Branch: "eqt/x", WorkspaceID: "w2"}}},
		{Name: "zzz-archived", Archived: true},
		{Name: "beta"},
	}
	ung := []liveWorkspace{{ID: "w9", Label: "scratch"}}
	rows := buildPickRows(sessions, ung)

	var keys []string
	for _, r := range rows {
		keys = append(keys, r.key)
	}
	joined := strings.Join(keys, "\n")
	for _, want := range []string{"session\talpha", "workspace\tw2\talpha", "session\tbeta", "workspace\tw9\t"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing row %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "zzz-archived") {
		t.Error("archived sessions should not be offered in the picker")
	}
	if !strings.Contains(rows[0].display, "●") {
		t.Errorf("active session should be marked, got %q", rows[0].display)
	}
}

func TestArchivedHiddenFromSidebar(t *testing.T) {
	m := demoSidebar()
	m.rows[0].session.Archived = true
	if m.showArchived {
		t.Fatal("archived should be hidden by default")
	}
	out := m.View()
	if !strings.Contains(out, "⌁") {
		t.Errorf("archived session should render with a marker when shown:\n%s", out)
	}
}
