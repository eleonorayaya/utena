package tui

import (
	"os"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type FilePickerModel struct {
	picker filepicker.Model
	width  int
	height int
}

func NewFilePickerModel() FilePickerModel {
	fp := filepicker.New()
	fp.DirAllowed = false
	fp.FileAllowed = false
	fp.ShowHidden = false
	homeDir, _ := os.UserHomeDir()
	fp.CurrentDirectory = homeDir

	return FilePickerModel{
		picker: fp,
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

func (m FilePickerModel) Keys() help.KeyMap {
	return filePickerKeys
}

func (m FilePickerModel) Update(msg tea.Msg) (FilePickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, filePickerKeys.SelectDir) {
			return m, func() tea.Msg {
				return directorySelectedMsg{path: m.picker.CurrentDirectory}
			}
		}
		if key.Matches(msg, filePickerKeys.ToggleHidden) {
			m.picker.ShowHidden = !m.picker.ShowHidden
			return m, m.picker.Init()
		}
	}

	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	return m, cmd
}

func (m FilePickerModel) View() string {
	return m.picker.View()
}
