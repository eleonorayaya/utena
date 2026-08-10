package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eleonorayaya/utena/internal/tui/theme"
)

// Ported from utena's sessionform: pick repos, then a branch per repo in
// sequence, then choose whether those branches are the base for new ones or
// are checked out as-is. A single shared branch cannot express the sessions
// that already exist — eight of them run different branches per repo.
type mode int

const (
	modeList mode = iota
	modePickRepos
	modeBranchPick
	modeBranchManual
	modeBranchMode
	modeName
)

type repoChoice struct {
	path     string
	selected bool
}

type createKeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Toggle  key.Binding
	Accept  key.Binding
	Back    key.Binding
	Fetch   key.Binding
	Manual  key.Binding
	NewMode key.Binding
	AsIs    key.Binding
}

func newCreateKeyMap() createKeyMap {
	return createKeyMap{
		Up:      key.NewBinding(key.WithKeys("k", "up")),
		Down:    key.NewBinding(key.WithKeys("j", "down")),
		Toggle:  key.NewBinding(key.WithKeys(" ")),
		Accept:  key.NewBinding(key.WithKeys("enter")),
		Back:    key.NewBinding(key.WithKeys("esc")),
		Fetch:   key.NewBinding(key.WithKeys("r")),
		Manual:  key.NewBinding(key.WithKeys("n")),
		NewMode: key.NewBinding(key.WithKeys("n")),
		AsIs:    key.NewBinding(key.WithKeys("e")),
	}
}

type activatedMsg struct{ err error }

func (m sidebar) activateCmd(name string) tea.Cmd {
	h := m.herdr
	return func() tea.Msg { return activatedMsg{err: activateSession(h, name)} }
}

type createdMsg struct {
	name string
	err  error
}

type branchesMsg struct {
	repo     string
	branches []string
}

type branchCheckedMsg struct {
	name          string
	local, remote bool
}

func (m sidebar) startCreate() sidebar {
	paths := discoverRepos()
	m.repos = make([]repoChoice, 0, len(paths))
	for _, p := range paths {
		m.repos = append(m.repos, repoChoice{path: p})
	}
	m.repoCursor = 0
	m.mode = modePickRepos
	m.status = ""
	if len(m.repos) == 0 {
		m.status = "no git repos found under " + strings.Join(repoRoots(), ", ")
		m.mode = modeList
	}
	return m
}

func (m sidebar) selectedRepos() []string {
	var out []string
	for _, r := range m.repos {
		if r.selected {
			out = append(out, r.path)
		}
	}
	return out
}

func loadBranchesCmd(repo string, fetch bool) tea.Cmd {
	return func() tea.Msg {
		if fetch {
			_ = fetchOrigin(repo)
		}
		seen := map[string]struct{}{}
		var out []string
		for _, b := range append(localBranches(repo), remoteBranches(repo)...) {
			if _, dup := seen[b]; dup || b == "" {
				continue
			}
			seen[b] = struct{}{}
			out = append(out, b)
		}
		return branchesMsg{repo: repo, branches: out}
	}
}

func checkBranchCmd(repo, name string) tea.Cmd {
	return func() tea.Msg {
		l, r := branchExistsAnywhere(repo, name)
		return branchCheckedMsg{name: name, local: l, remote: r}
	}
}

// startBranchPicking mirrors utena's method of the same name: a fresh picker
// per repo, titled with its position in the sequence.
func (m sidebar) startBranchPicking(index int) (sidebar, tea.Cmd) {
	m.pickingFor = index
	m.branchCursor = 0
	m.branches = nil
	m.mode = modeBranchPick
	return m, loadBranchesCmd(m.selectedRepos()[index], false)
}

func (m sidebar) currentPickRepo() string {
	sel := m.selectedRepos()
	if m.pickingFor < 0 || m.pickingFor >= len(sel) {
		return ""
	}
	return sel[m.pickingFor]
}

func (m sidebar) advanceBranch(branch string) (tea.Model, tea.Cmd) {
	m.picked[m.pickingFor] = branch
	if m.pickingFor+1 < len(m.selectedRepos()) {
		next, cmd := m.startBranchPicking(m.pickingFor + 1)
		return next, cmd
	}
	m.mode = modeBranchMode
	return m, nil
}

func (m sidebar) updatePickRepos(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.createKeys.Back):
		m.mode = modeList
		m.status = ""

	case key.Matches(msg, m.createKeys.Up):
		if m.repoCursor > 0 {
			m.repoCursor--
		}

	case key.Matches(msg, m.createKeys.Down):
		if m.repoCursor < len(m.repos)-1 {
			m.repoCursor++
		}

	case key.Matches(msg, m.createKeys.Toggle):
		if m.repoCursor < len(m.repos) {
			m.repos[m.repoCursor].selected = !m.repos[m.repoCursor].selected
		}

	case key.Matches(msg, m.createKeys.Accept):
		if len(m.selectedRepos()) == 0 && m.repoCursor < len(m.repos) {
			m.repos[m.repoCursor].selected = true
		}
		sel := m.selectedRepos()
		if len(sel) == 0 {
			break
		}
		m.picked = make([]string, len(sel))
		return m.startBranchPicking(0)
	}
	return m, nil
}

func (m sidebar) updateBranchPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.createKeys.Back):
		if m.pickingFor == 0 {
			m.mode = modePickRepos
			m.picked = nil
			return m, nil
		}
		return m.startBranchPicking(m.pickingFor - 1)

	case key.Matches(msg, m.createKeys.Up):
		if m.branchCursor > 0 {
			m.branchCursor--
		}

	case key.Matches(msg, m.createKeys.Down):
		if m.branchCursor < len(m.branches)-1 {
			m.branchCursor++
		}

	case key.Matches(msg, m.createKeys.Fetch):
		m.status = "fetching origin…"
		return m, loadBranchesCmd(m.currentPickRepo(), true)

	case key.Matches(msg, m.createKeys.Manual):
		ti := textinput.New()
		ti.Prompt = "branch: "
		ti.Focus()
		m.branchInput = ti
		m.branchErr = ""
		m.pendingCheck = ""
		m.mode = modeBranchManual
		return m, textinput.Blink

	case key.Matches(msg, m.createKeys.Accept):
		if m.branchCursor < len(m.branches) {
			return m.advanceBranch(m.branches[m.branchCursor])
		}
	}
	return m, nil
}

func (m sidebar) updateBranchManual(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(km, m.createKeys.Back):
			m.mode = modeBranchPick
			m.branchErr = ""
			m.pendingCheck = ""
			return m, nil
		case key.Matches(km, m.createKeys.Accept):
			name := strings.TrimSpace(m.branchInput.Value())
			if name == "" {
				m.branchErr = "branch name required"
				return m, nil
			}
			m.branchErr = ""
			m.pendingCheck = name
			return m, checkBranchCmd(m.currentPickRepo(), name)
		}
	}
	var cmd tea.Cmd
	m.branchInput, cmd = m.branchInput.Update(msg)
	return m, cmd
}

// onBranchChecked guards against a stale response, as utena's did.
func (m sidebar) onBranchChecked(msg branchCheckedMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeBranchManual || m.pendingCheck == "" || msg.name != m.pendingCheck {
		return m, nil
	}
	m.pendingCheck = ""
	if !msg.local && !msg.remote {
		m.branchErr = "branch not found: " + msg.name
		return m, nil
	}
	m.branchErr = ""
	return m.advanceBranch(msg.name)
}

func (m sidebar) updateBranchMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.createKeys.NewMode):
		ti := textinput.New()
		ti.Prompt = "session name: "
		if sel := m.selectedRepos(); len(sel) > 0 {
			ti.Placeholder = sanitizeName(filepath.Base(sel[0]))
		}
		ti.Focus()
		m.nameInput = ti
		m.nameErr = ""
		m.mode = modeName
		return m, textinput.Blink

	case key.Matches(msg, m.createKeys.AsIs):
		m.mode = modeList
		m.status = "creating session…"
		return m, m.createCmd("", false)

	case key.Matches(msg, m.createKeys.Back):
		return m.startBranchPicking(len(m.selectedRepos()) - 1)
	}
	return m, nil
}

func (m sidebar) updateName(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(km, m.createKeys.Back):
			m.mode = modeBranchMode
			m.nameErr = ""
			return m, nil
		case key.Matches(km, m.createKeys.Accept):
			name := strings.TrimSpace(m.nameInput.Value())
			if name == "" {
				name = m.nameInput.Placeholder
			}
			if name == "" {
				m.nameErr = "session name required"
				return m, nil
			}
			m.mode = modeList
			m.status = "creating session…"
			return m, m.createCmd(name, true)
		}
	}
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return m, cmd
}

// createCmd mirrors buildSpecs: in new mode the picked branch is the base and
// the session name becomes the branch; otherwise the pick is checked out as-is.
func (m sidebar) createCmd(name string, isNew bool) tea.Cmd {
	h := m.herdr
	sel := append([]string(nil), m.selectedRepos()...)
	picked := append([]string(nil), m.picked...)
	return func() tea.Msg {
		in := createInput{Name: name}
		for i, repo := range sel {
			spec := checkoutSpec{Repo: repo}
			if isNew {
				spec.Branch, spec.Base = sanitizeBranch(name), picked[i]
			} else {
				spec.Branch = picked[i]
			}
			in.Checkouts = append(in.Checkouts, spec)
		}
		sess, err := createSession(h, in)
		if err != nil {
			return createdMsg{err: err}
		}
		return createdMsg{name: sess.Name}
	}
}

func sanitizeBranch(name string) string {
	return strings.Trim(strings.ReplaceAll(strings.TrimSpace(name), " ", "-"), "/")
}

func (m sidebar) viewPickRepos() string {
	t := theme.Current
	var b strings.Builder
	b.WriteString(headerStyle(m.width).Render("select repos") + "\n")

	body := m.bodyHeight() - 1
	start := 0
	if m.repoCursor >= body {
		start = m.repoCursor - body + 1
	}
	for i := start; i < len(m.repos) && i < start+body; i++ {
		r := m.repos[i]
		mark := "  "
		if r.selected {
			mark = lipgloss.NewStyle().Foreground(t.AccentMint).Render("✓ ")
		}
		line := fitLine(mark+filepath.Base(r.path), m.width)
		style := lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Padding(0, 1)
		if i == m.repoCursor {
			line = ansiStrip(line)
			style = style.Background(t.SurfaceActive).Foreground(t.TextEmphasis).Bold(true)
		}
		b.WriteString(style.Render(line) + "\n")
	}
	return b.String() + footer(m.width,
		fmt.Sprintf("%d selected · space toggle · enter next · esc back", len(m.selectedRepos())))
}

func (m sidebar) viewBranchPick() string {
	t := theme.Current
	sel := m.selectedRepos()
	var b strings.Builder
	b.WriteString(headerStyle(m.width).Render(fmt.Sprintf("branch for %s (%d/%d)",
		filepath.Base(m.currentPickRepo()), m.pickingFor+1, len(sel))) + "\n")

	body := m.bodyHeight() - 1
	if len(m.branches) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(t.TextMuted).Padding(0, 1).
			Render("loading branches…") + "\n")
	}
	start := 0
	if m.branchCursor >= body {
		start = m.branchCursor - body + 1
	}
	for i := start; i < len(m.branches) && i < start+body; i++ {
		line := fitLine(m.branches[i], m.width)
		style := lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Padding(0, 1)
		if i == m.branchCursor {
			style = style.Background(t.SurfaceActive).Foreground(t.TextEmphasis).Bold(true)
		}
		b.WriteString(style.Render(line) + "\n")
	}
	return b.String() + footer(m.width, "enter pick · r fetch origin · n type a name · esc back")
}

func (m sidebar) viewBranchManual() string {
	t := theme.Current
	var b strings.Builder
	b.WriteString(headerStyle(m.width).Render("branch name") + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(t.TextMuted).Padding(0, 1).
		Render("for "+filepath.Base(m.currentPickRepo())) + "\n\n")
	b.WriteString(lipgloss.NewStyle().Padding(0, 1).Render(m.branchInput.View()) + "\n")
	if m.pendingCheck != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(t.TextMuted).Padding(0, 1).
			Render("checking…") + "\n")
	}
	if m.branchErr != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(t.Error).Padding(0, 1).
			Render(m.branchErr) + "\n")
	}
	return b.String() + footer(m.width, "enter confirm · esc back")
}

func (m sidebar) branchSummary() string {
	t := theme.Current
	var b strings.Builder
	for i, repo := range m.selectedRepos() {
		br := ""
		if i < len(m.picked) {
			br = m.picked[i]
		}
		b.WriteString(lipgloss.NewStyle().Padding(0, 1).Render(fitLine(
			lipgloss.NewStyle().Foreground(t.Text).Render(filepath.Base(repo))+"  "+
				lipgloss.NewStyle().Foreground(t.AccentBlue).Render(br), m.width)) + "\n")
	}
	return b.String()
}

func (m sidebar) viewBranchMode() string {
	t := theme.Current
	var b strings.Builder
	b.WriteString(headerStyle(m.width).Render("new session") + "\n")
	b.WriteString(m.branchSummary() + "\n")
	k := lipgloss.NewStyle().Foreground(t.TextEmphasis)
	d := lipgloss.NewStyle().Foreground(t.Text)
	b.WriteString(lipgloss.NewStyle().Padding(0, 1).Render(
		k.Render("n")+"  "+d.Render("create new branches from these")) + "\n")
	b.WriteString(lipgloss.NewStyle().Padding(0, 1).Render(
		k.Render("e")+"  "+d.Render("use these branches as-is")) + "\n")
	return b.String() + footer(m.width, "esc back")
}

func (m sidebar) viewName() string {
	t := theme.Current
	var b strings.Builder
	b.WriteString(headerStyle(m.width).Render("new session") + "\n")
	b.WriteString(m.branchSummary() + "\n")
	b.WriteString(lipgloss.NewStyle().Padding(0, 1).Render(m.nameInput.View()) + "\n")
	if m.nameErr != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(t.Error).Padding(0, 1).
			Render(m.nameErr) + "\n")
	}
	return b.String() + footer(m.width, "enter create · esc back")
}

func footer(width int, text string) string {
	return lipgloss.NewStyle().Foreground(theme.Current.Text).
		Padding(0, 1).Render(fitLine(text, width))
}

func headerStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(theme.Current.TextOnPrimary).
		Background(theme.Current.Primary).
		Bold(true).Width(width).MaxWidth(width).Padding(0, 1)
}
