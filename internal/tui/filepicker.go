package tui

import (
	"os"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type filePickerPhase int

const (
	pickingDirectory filePickerPhase = iota
	choosingType
)

type switchToFilePickerMsg struct{}
type filePickerDoneMsg struct{}

type FilePickerModel struct {
	picker       filepicker.Model
	phase        filePickerPhase
	selectedPath string
	err          string
	width        int
	height       int
}

func NewFilePickerModel() FilePickerModel {
	fp := filepicker.New()
	fp.DirAllowed = false
	fp.FileAllowed = false
	fp.ShowHidden = false
	fp.SetHeight(15)
	homeDir, _ := os.UserHomeDir()
	fp.CurrentDirectory = homeDir

	return FilePickerModel{
		picker: fp,
		phase:  pickingDirectory,
	}
}

func (m *FilePickerModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.picker.SetHeight(height - 4)
}

func (m FilePickerModel) Init() tea.Cmd {
	return m.picker.Init()
}

func (m FilePickerModel) Update(msg tea.Msg) (FilePickerModel, tea.Cmd) {
	switch m.phase {
	case pickingDirectory:
		return m.updatePicking(msg)
	case choosingType:
		return m.updateChoosing(msg)
	}
	return m, nil
}

func (m FilePickerModel) updatePicking(msg tea.Msg) (FilePickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, backKey) {
			return m, func() tea.Msg { return switchToNewSessionMsg{} }
		}
		if key.Matches(msg, selectDirKey) {
			m.selectedPath = m.picker.CurrentDirectory
			m.phase = choosingType
			return m, nil
		}
		if key.Matches(msg, toggleHiddenKey) {
			m.picker.ShowHidden = !m.picker.ShowHidden
			return m, m.picker.Init()
		}
	}

	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	return m, cmd
}

var (
	promptStyle = lipgloss.NewStyle().Bold(true)
	pathStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
)

func (m FilePickerModel) updateChoosing(msg tea.Msg) (FilePickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "w":
			return m, addWorkspace(m.selectedPath, false)
		case "r":
			return m, addWorkspace(m.selectedPath, true)
		case "esc":
			m.phase = pickingDirectory
			m.selectedPath = ""
			return m, nil
		}
	}
	return m, nil
}

func (m FilePickerModel) View() string {
	switch m.phase {
	case choosingType:
		return promptStyle.Render("Add: ") + pathStyle.Render(abbreviatePath(m.selectedPath)) +
			"\n\n" +
			"  w  add as workspace\n" +
			"  r  add as root (scan subdirectories)\n\n" +
			"  esc: back"
	default:
		return m.picker.View() + "\n\n  s: select dir  .: toggle hidden  esc: back"
	}
}
