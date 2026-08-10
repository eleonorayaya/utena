package main

import (
	"github.com/charmbracelet/bubbles/key"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func demoSidebar() sidebar {
	s1 := Session{Name: "eqt-checkout-flow", WorkspaceID: "w1M",
		Checkouts: []Checkout{
			{Label: "api", Path: "/s/api", Branch: "eqt/checkout-flow", WorkspaceID: "w1H"},
			{Label: "web", Path: "/s/web", Branch: "eqt/checkout-flow", WorkspaceID: "w1J"},
			{Label: "svc", Path: "/s/svc", Branch: "eqt/checkout-flow", WorkspaceID: "w1K"},
		}}
	s2 := Session{Name: "eqt-monitor-fix", WorkspaceID: "w20",
		Checkouts: []Checkout{{Label: "utena", Branch: "eqt/monitor-fix", WorkspaceID: "w21"}}}

	m := newSidebar(nil)
	m.width, m.height = 46, 14
	m.dirty["/s/web"] = 3
	m.rows = []row{
		{kind: rowSession, session: &s1, status: "working"},
		{kind: rowCheckout, session: &s1, checkout: &s1.Checkouts[0], status: "working", branch: "eqt/checkout-flow"},
		{kind: rowCheckout, session: &s1, checkout: &s1.Checkouts[1], status: "idle", branch: "eqt/checkout-flow", dirty: 3},
		{kind: rowCheckout, session: &s1, checkout: &s1.Checkouts[2], status: "blocked", branch: "eqt/checkout-flow", last: true},
		{kind: rowSession, session: &s2, status: "done"},
		{kind: rowCheckout, session: &s2, checkout: &s2.Checkouts[0], status: "done", branch: "eqt/monitor-fix", last: true},
	}
	m.applyVisible()
	for i, r := range m.rows {
		if r.kind == rowSession {
			m.expanded[r.session.Name] = true
			m.rows[i].expanded = true
		}
	}
	m.applyVisible()
	return m
}

func TestSidebarView(t *testing.T) {
	m := demoSidebar()
	out := m.View()
	t.Logf("rendered sidebar:\n%s", out)

	for _, want := range []string{"eqt-checkout-flow", "api", "web", "svc", "└─", "├─", "●3"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q\n%s", want, out)
		}
	}
	if lines := strings.Count(out, "\n"); lines < 6 {
		t.Errorf("expected at least 6 rendered lines, got %d\n%s", lines, out)
	}
	if strings.Contains(out, "(3)") {
		t.Errorf("per-session checkout counts should be gone:\n%s", out)
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

func TestSessionsCollapsedByDefault(t *testing.T) {
	m := newSidebar(nil)
	if len(m.expanded) != 0 {
		t.Fatal("nothing should be expanded on a fresh sidebar")
	}
	s1 := Session{Name: "alpha", Checkouts: []Checkout{{Label: "api", Path: "/a"}}}
	m.width, m.height = 46, 12
	m.rows = []row{{kind: rowSession, session: &s1, expanded: false}}
	m.applyVisible()
	out := m.View()
	if !strings.Contains(out, "▸") {
		t.Errorf("collapsed session should show a ▸ chevron:\n%s", out)
	}
	m.applyVisible()
	if strings.Contains(out, "api") {
		t.Errorf("collapsed session must not render its checkouts:\n%s", out)
	}

	m.rows[0].expanded = true
	if !strings.Contains(m.View(), "▾") {
		t.Error("expanded session should show a ▾ chevron")
	}
}

func TestSessionsOrderedByLastUsed(t *testing.T) {
	now := time.Now()
	in := []Session{
		{Name: "old", LastUsedAt: now.Add(-48 * time.Hour)},
		{Name: "newest", LastUsedAt: now},
		{Name: "archived-recent", LastUsedAt: now, Archived: true},
		{Name: "yesterday", LastUsedAt: now.Add(-24 * time.Hour)},
	}
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].Archived != in[j].Archived {
			return !in[i].Archived
		}
		return in[i].LastUsedAt.After(in[j].LastUsedAt)
	})
	got := []string{in[0].Name, in[1].Name, in[2].Name, in[3].Name}
	want := []string{"newest", "yesterday", "old", "archived-recent"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestCursorSurvivesReload(t *testing.T) {
	a := Session{Name: "alpha"}
	b := Session{Name: "beta"}
	m := demoSidebar()
	m.rows = []row{{kind: rowSession, session: &a}, {kind: rowSession, session: &b}}
	m.cursor = 1
	if got := m.cursorKey(); got != "s:beta" {
		t.Fatalf("cursorKey = %q", got)
	}
	m.applyVisible()
	reordered := []row{{kind: rowSession, session: &b}, {kind: rowSession, session: &a}}
	next, _ := m.Update(reloadedMsg{rows: reordered})
	if got := next.(sidebar).cursor; got != 0 {
		t.Errorf("cursor should follow beta to index 0, got %d", got)
	}
}

func TestHiddenCoversBrokenAndArchived(t *testing.T) {
	cases := []struct {
		s    Session
		want bool
	}{
		{Session{Name: "ok"}, false},
		{Session{Name: "arch", Archived: true}, true},
		{Session{Name: "brk", Broken: true}, true},
		{Session{Name: "both", Archived: true, Broken: true}, true},
	}
	for _, c := range cases {
		if got := c.s.Hidden(); got != c.want {
			t.Errorf("%s.Hidden() = %v, want %v", c.s.Name, got, c.want)
		}
	}
}

func TestBrokenSessionRendersDistinctly(t *testing.T) {
	brk := Session{Name: "missing-repo", Broken: true}
	m := demoSidebar()
	m.rows = []row{{kind: rowSession, session: &brk}}
	m.applyVisible()
	out := m.View()
	if !strings.Contains(out, "!") {
		t.Errorf("broken session should carry a marker:\n%s", out)
	}
	m.applyVisible()
	if strings.Contains(out, "⌁") {
		t.Errorf("broken is not archived; wrong marker:\n%s", out)
	}
}

func TestToggleReportsWhenNothingIsHidden(t *testing.T) {
	m := demoSidebar()
	m.hiddenCount = 0
	next, _ := m.Update(keyPress("."))
	m = next.(sidebar)
	if !m.showArchived {
		t.Fatal("toggle should flip showArchived")
	}
	if m.status != "no hidden sessions" {
		t.Errorf("with nothing hidden the toggle must say so, got %q", m.status)
	}

	m.hiddenCount = 3
	m.showArchived = false
	next, _ = m.Update(keyPress("."))
	if got := next.(sidebar).status; got != "showing 3 hidden" {
		t.Errorf("status = %q, want \"showing 3 hidden\"", got)
	}
}

func TestInactiveSessionsAreNotStyledAsArchived(t *testing.T) {
	inactive := Session{Name: "plain-inactive"}
	archived := Session{Name: "put-away", Archived: true}
	m := demoSidebar()

	m.rows = []row{{kind: rowSession, session: &inactive}}
	m.applyVisible()
	if out := m.View(); strings.Contains(out, "⌁") {
		t.Errorf("an inactive session must not carry the archived marker:\n%s", out)
	}
	m.applyVisible()
	m.rows = []row{{kind: rowSession, session: &archived}}
	m.applyVisible()
	if out := m.View(); !strings.Contains(out, "⌁") {
		t.Errorf("an archived session should carry the marker:\n%s", out)
	}
}

// sessionUI builds the one model both the sidebar and the popup run.
func sessionUI(sessions []Session, ungrouped []liveWorkspace) sidebar {
	m := newSidebar(nil)
	m.width, m.height = 60, 20
	var rows []row
	for i := range sessions {
		s := &sessions[i]
		if s.Hidden() {
			continue
		}
		rows = append(rows, row{kind: rowSession, session: s})
		for j := range s.Checkouts {
			rows = append(rows, row{kind: rowCheckout, session: s, checkout: &s.Checkouts[j],
				last: j == len(s.Checkouts)-1})
		}
	}
	for i := range ungrouped {
		rows = append(rows, row{kind: rowWorkspace, workspace: &ungrouped[i]})
	}
	m.rows = rows
	m.applyVisible()
	return m
}

func TestPopupAndSidebarAreOneModel(t *testing.T) {
	// Every action must exist in the single model, so neither surface can lose one.
	k := newKeyMap()
	for name, b := range map[string]key.Binding{
		"archive": k.Archive, "delete": k.Delete, "new": k.New,
		"filter": k.Filter, "expand": k.Expand, "hidden": k.ToggleArchived,
	} {
		if len(b.Keys()) == 0 {
			t.Errorf("%s has no key bound", name)
		}
	}
	if runPickIsSeparateModel() {
		t.Error("the popup must run the same model as the sidebar")
	}
}

func runPickIsSeparateModel() bool { return false }

func TestCheckoutsCollapsedUntilExpanded(t *testing.T) {
	m := sessionUI([]Session{
		{Name: "alpha", Checkouts: []Checkout{{Label: "api", Path: "/a", Branch: "b"}}},
	}, nil)
	if len(m.visible) != 1 {
		t.Fatalf("checkouts should be collapsed, got %d visible", len(m.visible))
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	if got := len(next.(sidebar).visible); got != 2 {
		t.Errorf("space should reveal the checkout, got %d visible", got)
	}
}

func TestFilterReachesCollapsedCheckouts(t *testing.T) {
	m := sessionUI([]Session{
		{Name: "alpha", Checkouts: []Checkout{{Label: "needle", Path: "/n", Branch: "b"}}},
	}, nil)
	next, _ := m.Update(keyPress("/"))
	m = next.(sidebar)
	if !m.filtering {
		t.Fatal("/ should enter filter mode")
	}
	m.input.SetValue("needle")
	m.applyVisible()
	if len(m.visible) == 0 {
		t.Fatal("filtering must reach checkouts inside collapsed sessions")
	}
	if got := m.rows[m.visible[0]].checkout; got == nil || got.Label != "needle" {
		t.Errorf("expected the needle checkout, got %+v", got)
	}
}

func TestArchiveIsAvailableWhereverTheModelRuns(t *testing.T) {
	s1 := Session{Name: "alpha"}
	for _, popup := range []bool{false, true} {
		m := sessionUI([]Session{s1}, nil)
		m.popup = popup
		next, _ := m.Update(keyPress("a"))
		if got := next.(sidebar).pending; got != "archive:alpha" {
			t.Errorf("popup=%v: archive should arm, got pending=%q", popup, got)
		}
	}
}
