package sessionpicker

import (
	"slices"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

type Model struct {
	title         string
	list          list.Model
	width, height int
}

func New(title string) Model {
	return Model{title: title, list: ui.NewList()}
}

func (m Model) Open(sessions []session.Session) Model {
	sorted := slices.Clone(sessions)
	slices.SortStableFunc(sorted, func(a, b session.Session) int {
		if a.IsAttached != b.IsAttached {
			if a.IsAttached {
				return -1
			}
			return 1
		}
		return b.LastUsedAt.Compare(a.LastUsedAt)
	})
	items := make([]list.Item, 0, len(sorted))
	for _, s := range sorted {
		items = append(items, sessionItem{session: s})
	}
	m.list.ResetFilter()
	ui.SetItems(&m.list, items)
	m.list.Select(0)
	return m
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.list.SetSize(width, max(height-2, 1))
}

func (m Model) CapturesText() bool {
	return ui.FilterTyping(m.list)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		if msg.Type == tea.KeyEsc && !ui.FilterActive(m.list) {
			return m, func() tea.Msg { return ClosedMsg{} }
		}
		if !ui.FilterTyping(m.list) && key.Matches(msg, Keys.Select) {
			item, ok := m.list.SelectedItem().(sessionItem)
			if !ok {
				return m, nil
			}
			return m, func() tea.Msg { return SelectedMsg{Session: item.session} }
		}
	}
	var cmd tea.Cmd
	m.list, cmd = ui.UpdateList(m.list, msg)
	return m, cmd
}

func (m Model) View() string {
	title := ui.Clamp(ui.FormTitle(m.title), m.width)
	body := ui.ListView(m.list)
	if len(m.list.Items()) == 0 {
		body = ui.Muted().Render("  No other sessions")
	}
	return ui.FitHeight(title+"\n\n"+body, m.height)
}
