package main

import (
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

func TestPickerExcludesArchivedAndFilters(t *testing.T) {
	sessions := []Session{
		{Name: "alpha", WorkspaceID: "w1", Checkouts: []Checkout{
			{Label: "api", Branch: "eqt/x", Path: "/s/api", WorkspaceID: "w2"}}},
		{Name: "zzz-archived", Archived: true},
		{Name: "beta"},
	}
	ung := []liveWorkspace{{ID: "w9", Label: "scratch"}}

	p := newPicker(sessions, ung)
	for _, i := range p.visible {
		if p.rows[i].kind == rowCheckout {
			t.Error("checkouts should be collapsed in the picker by default")
		}
	}
	for _, term := range p.terms {
		if strings.Contains(term, "zzz-archived") {
			t.Error("archived sessions must not be offered in the picker")
		}
	}

	p.input.SetValue("scratch")
	p.applyFilter()
	if len(p.visible) != 1 {
		t.Fatalf("expected 1 match for \"scratch\", got %d", len(p.visible))
	}
	if got := p.targets[p.visible[0]].workspaceID; got != "w9" {
		t.Errorf("filter matched the wrong row: %q", got)
	}

	p.input.SetValue("api")
	p.applyFilter()
	if len(p.visible) == 0 {
		t.Fatal("expected the checkout row to match \"api\"")
	}
	if got := p.targets[p.visible[0]].workspaceID; got != "w2" {
		t.Errorf("expected the api checkout, got %q", got)
	}
}

func TestPickerEnterSelectsCursorRow(t *testing.T) {
	p := newPicker([]Session{{Name: "alpha", WorkspaceID: "w1"}}, nil)
	next, _ := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(picker).chosen
	if got == nil || got.workspaceID != "w1" {
		t.Errorf("enter should choose the cursor row, got %+v", got)
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

func TestPickerView(t *testing.T) {
	sessions := []Session{
		{Name: "eqt-checkout-flow", WorkspaceID: "w1", Checkouts: []Checkout{
			{Label: "api", Branch: "eqt/checkout-flow", Path: "/s/api", WorkspaceID: "w2"},
			{Label: "web", Branch: "eqt/checkout-flow", Path: "/s/web", WorkspaceID: "w3"}}},
		{Name: "monitor-scripts", Checkouts: []Checkout{
			{Label: "utena", Branch: "eqt/monitor-fix", Path: "/s/u"}}},
	}
	p := newPicker(sessions, []liveWorkspace{{ID: "w9", Label: "helm"}})
	p.width, p.height = 54, 12
	t.Logf("picker collapsed:\n%s", p.View())
	for _, want := range []string{"go to", "eqt-checkout-flow", "▸", "helm", "/ filter"} {
		if !strings.Contains(p.View(), want) {
			t.Errorf("missing %q in collapsed view", want)
		}
	}
	if strings.Contains(p.View(), "├─") {
		t.Error("collapsed picker should not render checkouts")
	}

	p.expanded["eqt-checkout-flow"] = true
	p.applyFilter()
	t.Logf("picker expanded:\n%s", p.View())
	for _, want := range []string{"▾", "├─", "└─", "api"} {
		if !strings.Contains(p.View(), want) {
			t.Errorf("missing %q in expanded view", want)
		}
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
	out := m.View()
	if !strings.Contains(out, "▸") {
		t.Errorf("collapsed session should show a ▸ chevron:\n%s", out)
	}
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
	out := m.View()
	if !strings.Contains(out, "!") {
		t.Errorf("broken session should carry a marker:\n%s", out)
	}
	if strings.Contains(out, "⌁") {
		t.Errorf("broken is not archived; wrong marker:\n%s", out)
	}
}

func TestPickerExcludesBroken(t *testing.T) {
	p := newPicker([]Session{
		{Name: "fine"},
		{Name: "busted", Broken: true},
	}, nil)
	for _, term := range p.terms {
		if strings.Contains(term, "busted") {
			t.Error("broken sessions must not be offered in the picker")
		}
	}
}

func TestPickerCollapseAndHiddenMatchSidebar(t *testing.T) {
	sessions := []Session{
		{Name: "alpha", Checkouts: []Checkout{{Label: "api", Path: "/a"}}},
		{Name: "old", Archived: true},
		{Name: "busted", Broken: true},
	}
	p := newPicker(sessions, nil)

	if len(p.visible) != 1 {
		t.Fatalf("only the one healthy session should show collapsed, got %d", len(p.visible))
	}

	next, _ := p.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	p = next.(picker)
	if len(p.visible) != 2 {
		t.Errorf("space should reveal the checkout, got %d rows", len(p.visible))
	}

	next, _ = p.Update(keyPress("."))
	p = next.(picker)
	names := ""
	for _, i := range p.visible {
		if p.rows[i].session != nil {
			names += p.rows[i].session.Name + " "
		}
	}
	for _, want := range []string{"alpha", "old", "busted"} {
		if !strings.Contains(names, want) {
			t.Errorf(". should reveal hidden sessions; %q missing from %q", want, names)
		}
	}
}

func TestPickerFilterFindsCollapsedCheckouts(t *testing.T) {
	p := newPicker([]Session{
		{Name: "alpha", Checkouts: []Checkout{{Label: "needle", Path: "/n", WorkspaceID: "w7"}}},
	}, nil)
	if len(p.visible) != 1 {
		t.Fatalf("expected the checkout collapsed, got %d rows", len(p.visible))
	}
	p.input.SetValue("needle")
	p.applyFilter()
	if len(p.visible) == 0 {
		t.Fatal("filtering must reach checkouts inside collapsed sessions")
	}
	if got := p.targets[p.visible[0]].workspaceID; got != "w7" {
		t.Errorf("expected the needle checkout, got %q", got)
	}
}
