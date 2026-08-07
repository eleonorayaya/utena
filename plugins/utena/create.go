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

type mode int

const (
	modeList mode = iota
	modePickRepos
	modeBranch
)

type repoChoice struct {
	path     string
	selected bool
}

type createKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Toggle key.Binding
	Accept key.Binding
	Back   key.Binding
}

func newCreateKeyMap() createKeyMap {
	return createKeyMap{
		Up:     key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
		Down:   key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
		Toggle: key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
		Accept: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "next")),
		Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
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

func (m sidebar) createCmd(branch string, repos []string) tea.Cmd {
	h := m.herdr
	return func() tea.Msg {
		sess, err := createSession(h, createInput{Branch: branch, Repos: repos})
		if err != nil {
			return createdMsg{err: err}
		}
		return createdMsg{name: sess.Name}
	}
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
		if len(m.selectedRepos()) == 0 {
			break
		}
		ti := textinput.New()
		ti.Placeholder = "eqt/my-feature"
		ti.Prompt = "branch: "
		ti.CharLimit = 80
		ti.Focus()
		m.branchInput = ti
		m.mode = modeBranch
	}
	return m, nil
}

func (m sidebar) updateBranch(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(km, m.createKeys.Back):
			m.mode = modePickRepos
			return m, nil
		case key.Matches(km, m.createKeys.Accept):
			branch := strings.TrimSpace(m.branchInput.Value())
			if branch == "" {
				return m, nil
			}
			repos := m.selectedRepos()
			m.mode = modeList
			m.status = fmt.Sprintf("creating %s…", branch)
			return m, m.createCmd(branch, repos)
		}
	}
	var cmd tea.Cmd
	m.branchInput, cmd = m.branchInput.Update(msg)
	return m, cmd
}

func (m sidebar) viewPickRepos() string {
	t := theme.Current
	var b strings.Builder
	b.WriteString(headerStyle(m.width).Render("select repos") + "\n")

	body := m.bodyHeight()
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
		line := mark + filepath.Base(r.path)
		style := lipgloss.NewStyle().Width(m.width).Padding(0, 1)
		if i == m.repoCursor {
			style = style.Background(t.Selection)
		}
		b.WriteString(style.Render(line) + "\n")
	}

	foot := fmt.Sprintf("%d selected · space toggle · enter next · esc back", len(m.selectedRepos()))
	b.WriteString(lipgloss.NewStyle().Foreground(t.TextMuted).Padding(0, 1).Render(foot))
	return b.String()
}

func (m sidebar) viewBranch() string {
	t := theme.Current
	var b strings.Builder
	b.WriteString(headerStyle(m.width).Render("new session") + "\n\n")

	for _, p := range m.selectedRepos() {
		b.WriteString(lipgloss.NewStyle().Foreground(t.TextMuted).Padding(0, 1).
			Render("• "+filepath.Base(p)) + "\n")
	}
	b.WriteString("\n" + lipgloss.NewStyle().Padding(0, 1).Render(m.branchInput.View()) + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(t.TextMuted).Padding(0, 1).
		Render("enter create · esc back"))
	return b.String()
}

func headerStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(theme.Current.TextOnPrimary).
		Background(theme.Current.Primary).
		Bold(true).Width(width).Padding(0, 1)
}
