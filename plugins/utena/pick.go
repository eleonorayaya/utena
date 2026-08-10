package main

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/eleonorayaya/utena/internal/tui/theme"
)

type pickTarget struct {
	session     string
	workspaceID string
}

type pickerKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Top    key.Binding
	Bottom key.Binding
	Filter key.Binding
	Accept key.Binding
	Back   key.Binding
	Quit   key.Binding
}

func newPickerKeyMap() pickerKeyMap {
	return pickerKeyMap{
		Up:     key.NewBinding(key.WithKeys("k", "up")),
		Down:   key.NewBinding(key.WithKeys("j", "down")),
		Top:    key.NewBinding(key.WithKeys("g")),
		Bottom: key.NewBinding(key.WithKeys("G")),
		Filter: key.NewBinding(key.WithKeys("/")),
		Accept: key.NewBinding(key.WithKeys("enter")),
		Back:   key.NewBinding(key.WithKeys("esc")),
		Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c")),
	}
}

type picker struct {
	rows      []row
	targets   []pickTarget
	terms     []string
	visible   []int
	cursor    int
	offset    int
	filtering bool
	input     textinput.Model
	keys      pickerKeyMap
	width     int
	height    int
	chosen    *pickTarget
}

func buildPickerRows(sessions []Session, ungrouped []liveWorkspace) ([]row, []pickTarget, []string) {
	var rows []row
	var targets []pickTarget
	var terms []string

	for i := range sessions {
		s := &sessions[i]
		if s.Archived {
			continue
		}
		rows = append(rows, row{kind: rowSession, session: s, status: s.AgentStatus})
		targets = append(targets, pickTarget{session: s.Name, workspaceID: s.WorkspaceID})
		terms = append(terms, s.Name)

		for j := range s.Checkouts {
			c := &s.Checkouts[j]
			rows = append(rows, row{kind: rowCheckout, session: s, checkout: c,
				status: c.AgentStatus, last: j == len(s.Checkouts)-1})
			targets = append(targets, pickTarget{session: s.Name, workspaceID: c.WorkspaceID})
			terms = append(terms, s.Name+" "+c.Label+" "+c.Branch)
		}
	}
	for i := range ungrouped {
		w := &ungrouped[i]
		rows = append(rows, row{kind: rowWorkspace, workspace: w, status: w.AgentStatus})
		targets = append(targets, pickTarget{workspaceID: w.ID})
		terms = append(terms, w.Label)
	}
	return rows, targets, terms
}

func newPicker(sessions []Session, ungrouped []liveWorkspace) picker {
	rows, targets, terms := buildPickerRows(sessions, ungrouped)
	ti := textinput.New()
	ti.Prompt = "/"
	ti.Placeholder = "filter"

	p := picker{rows: rows, targets: targets, terms: terms,
		input: ti, keys: newPickerKeyMap(), width: 72, height: 20}
	p.applyFilter()
	return p
}

func (p *picker) applyFilter() {
	q := strings.TrimSpace(p.input.Value())
	p.visible = p.visible[:0]
	if q == "" {
		for i := range p.rows {
			p.visible = append(p.visible, i)
		}
	} else {
		for _, m := range fuzzy.Find(q, p.terms) {
			p.visible = append(p.visible, m.Index)
		}
	}
	if p.cursor >= len(p.visible) {
		p.cursor = len(p.visible) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	p.clamp()
}

func (p *picker) clamp() {
	body := p.bodyHeight()
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+body {
		p.offset = p.cursor - body + 1
	}
	if p.offset < 0 {
		p.offset = 0
	}
}

func (p picker) bodyHeight() int {
	h := p.height - 3
	if h < 1 {
		return 1
	}
	return h
}

func (p picker) Init() tea.Cmd { return textinput.Blink }

func (p picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		p.clamp()
		return p, nil

	case tea.KeyMsg:
		if p.filtering {
			switch {
			case key.Matches(msg, p.keys.Back):
				p.filtering = false
				p.input.Blur()
				p.input.SetValue("")
				p.applyFilter()
				return p, nil
			case key.Matches(msg, p.keys.Accept):
				return p.choose()
			}
			var cmd tea.Cmd
			p.input, cmd = p.input.Update(msg)
			p.applyFilter()
			return p, cmd
		}

		switch {
		case key.Matches(msg, p.keys.Quit), key.Matches(msg, p.keys.Back):
			return p, tea.Quit
		case key.Matches(msg, p.keys.Filter):
			p.filtering = true
			p.input.Focus()
			return p, textinput.Blink
		case key.Matches(msg, p.keys.Accept):
			return p.choose()
		case key.Matches(msg, p.keys.Up):
			if p.cursor > 0 {
				p.cursor--
				p.clamp()
			}
		case key.Matches(msg, p.keys.Down):
			if p.cursor < len(p.visible)-1 {
				p.cursor++
				p.clamp()
			}
		case key.Matches(msg, p.keys.Top):
			p.cursor, p.offset = 0, 0
		case key.Matches(msg, p.keys.Bottom):
			p.cursor = len(p.visible) - 1
			p.clamp()
		}
	}
	return p, nil
}

func (p picker) choose() (tea.Model, tea.Cmd) {
	if p.cursor < 0 || p.cursor >= len(p.visible) {
		return p, tea.Quit
	}
	t := p.targets[p.visible[p.cursor]]
	p.chosen = &t
	return p, tea.Quit
}

func (p picker) View() string {
	t := theme.Current
	var b strings.Builder
	b.WriteString(headerStyle(p.width).Render("go to") + "\n")

	body := p.bodyHeight()
	for i := p.offset; i < len(p.visible) && i < p.offset+body; i++ {
		style := lipgloss.NewStyle().Width(p.width).MaxWidth(p.width).Padding(0, 1)
		if i == p.cursor {
			style = style.Background(t.Selection)
		}
		b.WriteString(style.Render(fitLine(rowLine(p.rows[p.visible[i]], nil), p.width)) + "\n")
	}
	if len(p.visible) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(t.TextMuted).Padding(0, 1).
			Render("no matches") + "\n")
	}

	foot := "j/k move · / filter · enter open · q quit"
	if p.filtering {
		foot = p.input.View()
	}
	b.WriteString(lipgloss.NewStyle().Foreground(t.TextMuted).
		Padding(0, 1).Render(fitLine(foot, p.width)))
	return b.String()
}

func runPick() error {
	if path, err := utenaThemePath(); err == nil {
		_ = theme.Load(path)
	}
	h := newHerdrClient()
	sessions, ungrouped, err := loadSessions(h)
	if err != nil {
		return err
	}

	final, err := tea.NewProgram(newPicker(sessions, ungrouped), tea.WithAltScreen()).Run()
	if err != nil {
		return err
	}
	chosen := final.(picker).chosen
	if chosen == nil {
		return nil
	}

	if chosen.workspaceID != "" {
		return h.focusWorkspace(chosen.workspaceID)
	}
	if chosen.session == "" {
		return nil
	}
	if err := activateSession(h, chosen.session); err != nil {
		return err
	}
	refreshed, _, err := loadSessions(h)
	if err != nil {
		return err
	}
	for _, s := range refreshed {
		if s.Name == chosen.session && s.WorkspaceID != "" {
			return h.focusWorkspace(s.WorkspaceID)
		}
	}
	return nil
}
