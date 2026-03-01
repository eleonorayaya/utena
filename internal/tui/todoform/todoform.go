package todoform

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/filepicker"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/workspacepicker"
	"github.com/eleonorayaya/utena/internal/workspace"
)

var (
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	promptStyle = lipgloss.NewStyle().Bold(true)
	pathStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
)

type step int

const (
	workspacePickerStep step = iota
	filePickerStep
	dirTypeChoiceStep
	nameInputStep
)

type Model struct {
	activeStep        step
	workspacePicker   workspacepicker.Model
	filePicker        filepicker.Model
	nameInput         textinput.Model
	descInput         textinput.Model
	focusIndex        int
	selectedWorkspace *workspace.Workspace
	selectedDirPath   string
	activeWorkspaceID string
	nameErr           string
	width, height     int
}

func New() Model {
	nameInput := textinput.New()
	nameInput.Prompt = "Name: "
	nameInput.Placeholder = "todo name"
	nameInput.Focus()

	descInput := textinput.New()
	descInput.Prompt = "Description: "
	descInput.Placeholder = "optional description"

	return Model{
		activeStep:      workspacePickerStep,
		workspacePicker: workspacepicker.New("Select workspace for todo", true),
		nameInput:       nameInput,
		descInput:       descInput,
	}
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.workspacePicker.SetSize(width, height)
	m.filePicker.SetSize(width, height)
}

func (m Model) Init() (Model, tea.Cmd) {
	m.activeStep = workspacePickerStep
	m.selectedWorkspace = nil
	m.selectedDirPath = ""
	m.focusIndex = 0
	m.nameErr = ""
	m.nameInput.SetValue("")
	m.descInput.SetValue("")
	return m, provider.FetchWorkspaces()
}

func (m Model) Keys() help.KeyMap {
	switch m.activeStep {
	case workspacePickerStep:
		return workspacepicker.Keys
	case filePickerStep:
		return filepicker.Keys
	case nameInputStep:
		return inputKeys
	default:
		return formKeys
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.OnWindowSizeMsg(msg)
	case provider.WorkspacesStateUpdatedMsg:
		m.activeWorkspaceID = msg.ActiveWorkspaceID
		var cmd tea.Cmd
		m.workspacePicker, cmd = m.workspacePicker.Update(msg)
		return m, cmd
	case provider.TodoCreatedMsg:
		return m, func() tea.Msg { return DoneMsg{} }
	case provider.ErrMsg:
		m.nameErr = msg.Err.Error()
		return m, nil
	}

	switch m.activeStep {
	case workspacePickerStep:
		return m.updateWorkspacePicker(msg)
	case filePickerStep:
		return m.updateFilePicker(msg)
	case dirTypeChoiceStep:
		return m.updateDirTypeChoice(msg)
	case nameInputStep:
		return m.updateNameInput(msg)
	}
	return m, nil
}

func (m Model) OnWindowSizeMsg(msg tea.WindowSizeMsg) (Model, tea.Cmd) {
	m.SetSize(msg.Width, msg.Height)
	return m, nil
}

func (m Model) OnKeyMsg(_ tea.KeyMsg) (Model, tea.Cmd, bool) {
	return m, nil, false
}

func (m Model) updateWorkspacePicker(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case workspacepicker.SelectedMsg:
		ws := msg.Workspace
		m.selectedWorkspace = &ws
		m.activeStep = nameInputStep
		m.focusIndex = 0
		m.nameInput.Focus()
		m.descInput.Blur()
		m.nameInput.SetValue("")
		m.descInput.SetValue("")
		m.nameErr = ""
		return m, textinput.Blink
	case workspacepicker.AddDirectoryMsg:
		m.filePicker = filepicker.New()
		m.filePicker.SetSize(m.width, m.height)
		m.activeStep = filePickerStep
		var cmd tea.Cmd
		m.filePicker, cmd = m.filePicker.Init()
		return m, cmd
	case tea.KeyMsg:
		if key.Matches(msg, formKeys.Back) {
			return m, func() tea.Msg { return BackMsg{} }
		}
	}

	var cmd tea.Cmd
	m.workspacePicker, cmd = m.workspacePicker.Update(msg)
	return m, cmd
}

func (m Model) updateFilePicker(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case filepicker.DirectorySelectedMsg:
		m.selectedDirPath = msg.Path
		m.activeStep = dirTypeChoiceStep
		return m, nil
	case tea.KeyMsg:
		if key.Matches(msg, formKeys.Back) {
			m.activeStep = workspacePickerStep
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.filePicker, cmd = m.filePicker.Update(msg)
	return m, cmd
}

func (m Model) updateDirTypeChoice(msg tea.Msg) (Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "w":
			m.activeStep = workspacePickerStep
			return m, provider.AddWorkspace(m.selectedDirPath, false)
		case "r":
			m.activeStep = workspacePickerStep
			return m, provider.AddWorkspace(m.selectedDirPath, true)
		case "esc":
			m.activeStep = filePickerStep
			return m, nil
		}
	}
	return m, nil
}

func (m Model) updateNameInput(msg tea.Msg) (Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(msg, inputKeys.Submit):
			name := m.nameInput.Value()
			if name == "" {
				m.nameErr = "name is required"
				return m, nil
			}
			m.nameErr = ""
			desc := m.descInput.Value()
			wsID := ""
			if m.selectedWorkspace != nil {
				wsID = m.selectedWorkspace.ID
			}
			return m, provider.CreateTodo(name, desc, wsID)
		case key.Matches(msg, inputKeys.Back):
			m.activeStep = workspacePickerStep
			return m, provider.RequestWorkspacesState()
		case key.Matches(msg, inputKeys.NextTab):
			if m.focusIndex == 0 {
				m.focusIndex = 1
				m.nameInput.Blur()
				m.descInput.Focus()
			} else {
				m.focusIndex = 0
				m.descInput.Blur()
				m.nameInput.Focus()
			}
			return m, nil
		}
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	if m.focusIndex == 0 {
		m.nameInput, cmd = m.nameInput.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		m.descInput, cmd = m.descInput.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	switch m.activeStep {
	case filePickerStep:
		return m.filePicker.View()
	case dirTypeChoiceStep:
		return promptStyle.Render("Add: ") + pathStyle.Render(workspacepicker.AbbreviatePath(m.selectedDirPath)) +
			"\n\n" +
			"  w  add as workspace\n" +
			"  r  add as root\n\n" +
			"  esc: back"
	case nameInputStep:
		wsName := ""
		if m.selectedWorkspace != nil {
			wsName = m.selectedWorkspace.Name
		}
		view := promptStyle.Render("Workspace: ") + wsName + "\n\n"
		view += m.nameInput.View() + "\n"
		view += m.descInput.View() + "\n"
		if m.nameErr != "" {
			view += errStyle.Render(m.nameErr)
		}
		return view
	default:
		return m.workspacePicker.View()
	}
}
