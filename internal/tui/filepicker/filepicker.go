package filepicker

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

type dirItem struct {
	name string
}

func (i dirItem) FilterValue() string { return i.name }

func (i dirItem) Cells(selected bool) []ui.Cell {
	style := lipgloss.NewStyle().Foreground(theme.Current.Text)
	if selected {
		style = lipgloss.NewStyle().Foreground(theme.Current.TextEmphasis).Bold(true)
	}
	return []ui.Cell{
		{Spans: []ui.Span{{Text: "▸", Style: lipgloss.NewStyle().Foreground(theme.Current.TextMuted)}}, Width: 2},
		{Spans: []ui.Span{{Text: i.name, Style: style}}},
	}
}

type Model struct {
	dir        string
	showHidden bool
	list       list.Model
	status     ui.Status
	width      int
	height     int
}

func New() Model {
	home, _ := os.UserHomeDir()
	return NewAt(home)
}

func NewAt(dir string) Model {
	return Model{dir: dir, list: ui.NewList()}
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.list.SetSize(width, max(height-2, 1))
}

func (m Model) Init() (Model, tea.Cmd) {
	m.load("")
	return m, nil
}

func (m Model) Dir() string {
	return m.dir
}

func (m Model) Keys() help.KeyMap {
	return Keys
}

func (m Model) Status() ui.Status {
	return m.status
}

func (m Model) Filtering() bool {
	return ui.FilterActive(m.list)
}

func (m Model) CapturesText() bool {
	return m.list.FilterState() == list.Filtering
}

func (m *Model) load(selectName string) {
	m.list.ResetFilter()
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		m.status = ui.Status{Text: err.Error(), Kind: ui.StatusError}
		m.list.SetItems(nil)
		return
	}
	m.status = ui.Status{}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && e.Type()&os.ModeSymlink == 0 {
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(m.dir, e.Name())); err != nil || !info.IsDir() {
				continue
			}
		}
		if !m.showHidden && strings.HasPrefix(e.Name(), ".") {
			continue
		}
		names = append(names, e.Name())
	}
	slices.SortFunc(names, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	items := make([]list.Item, len(names))
	sel := 0
	for i, n := range names {
		items[i] = dirItem{name: n}
		if n == selectName {
			sel = i
		}
	}
	m.list.SetItems(items)
	m.list.Select(sel)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok && m.list.FilterState() != list.Filtering {
		if mm, cmd, handled := m.onKey(msg); handled {
			return mm, cmd
		}
	}
	var cmd tea.Cmd
	m.list, cmd = ui.UpdateList(m.list, msg)
	return m, cmd
}

func (m Model) onKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, Keys.SelectDir):
		dir := m.dir
		return m, func() tea.Msg { return DirectorySelectedMsg{Path: dir} }, true
	case key.Matches(msg, Keys.ToggleHidden):
		m.showHidden = !m.showHidden
		m.load("")
		return m, nil, true
	case key.Matches(msg, Keys.Up):
		if m.list.FilterState() == list.FilterApplied && msg.String() == "backspace" {
			return m, nil, false
		}
		parent := filepath.Dir(m.dir)
		if parent == m.dir {
			return m, nil, true
		}
		child := filepath.Base(m.dir)
		m.dir = parent
		m.load(child)
		return m, nil, true
	case key.Matches(msg, Keys.Open):
		item, ok := m.list.SelectedItem().(dirItem)
		if !ok {
			return m, nil, true
		}
		m.dir = filepath.Join(m.dir, item.name)
		m.load("")
		return m, nil, true
	}
	return m, nil, false
}

func abbreviate(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

func (m Model) View() string {
	muted := lipgloss.NewStyle().Foreground(theme.Current.TextMuted)
	title := ui.FormTitle("Add directory") + "  " + muted.Render(abbreviate(m.dir))
	body := ui.ListView(m.list)
	if len(m.list.Items()) == 0 {
		body = muted.Render("  No subdirectories — press s to select this one")
	}
	return ui.FitHeight(lipgloss.NewStyle().MaxWidth(max(m.width, 1)).Render(title)+"\n\n"+body, m.height)
}
