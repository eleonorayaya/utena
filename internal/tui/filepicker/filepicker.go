package filepicker

import (
	"os"

	bubblefp "github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type Model struct {
	picker bubblefp.Model
	width  int
	height int
}

func New() Model {
	fp := bubblefp.New()
	fp.DirAllowed = false
	fp.FileAllowed = false
	fp.ShowHidden = false
	homeDir, _ := os.UserHomeDir()
	fp.CurrentDirectory = homeDir

	return Model{
		picker: fp,
	}
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.picker.SetHeight(height)
}

func (m Model) OnWindowSizeMsg(msg tea.WindowSizeMsg) (Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.picker.SetHeight(msg.Height - 4)
	return m, nil
}

func (m Model) Init() (Model, tea.Cmd) {
	return m, m.picker.Init()
}

func (m Model) Keys() help.KeyMap {
	return Keys
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		m, cmd, handled := m.OnKeyMsg(msg)
		if handled {
			return m, cmd
		}
	}

	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	return m, cmd
}

func (m Model) OnKeyMsg(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if key.Matches(msg, Keys.SelectDir) {
		return m, func() tea.Msg {
			return DirectorySelectedMsg{Path: m.picker.CurrentDirectory}
		}, true
	}
	if key.Matches(msg, Keys.ToggleHidden) {
		m.picker.ShowHidden = !m.picker.ShowHidden
		return m, m.picker.Init(), true
	}
	return m, nil, false
}

func (m Model) View() string {
	return m.picker.View()
}
