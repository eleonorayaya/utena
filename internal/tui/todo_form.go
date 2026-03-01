package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/workspace"
)

var (
	todoFormErrStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	todoFormPromptStyle = lipgloss.NewStyle().Bold(true)
	todoFormPathStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
)

type todoFormStep int

const (
	todoWorkspacePickerStep todoFormStep = iota
	todoFilePickerStep
	todoDirTypeChoiceStep
	todoNameInputStep
)

type TodoFormModel struct {
	activeStep        todoFormStep
	workspacePicker   WorkspacePickerModel
	filePicker        FilePickerModel
	nameInput         textinput.Model
	descInput         textinput.Model
	focusIndex        int
	selectedWorkspace *workspace.Workspace
	selectedDirPath   string
	activeWorkspaceID string
	nameErr           string
	width, height     int
}

func NewTodoFormModel() TodoFormModel {
	nameInput := textinput.New()
	nameInput.Prompt = "Name: "
	nameInput.Placeholder = "todo name"
	nameInput.Focus()

	descInput := textinput.New()
	descInput.Prompt = "Description: "
	descInput.Placeholder = "optional description"

	return TodoFormModel{
		activeStep:      todoWorkspacePickerStep,
		workspacePicker: NewWorkspacePickerModel("Select workspace for todo", true),
		nameInput:       nameInput,
		descInput:       descInput,
	}
}

func (m *TodoFormModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.workspacePicker.SetSize(width, height)
	m.filePicker.SetSize(width, height)
}

func (m *TodoFormModel) Init() tea.Cmd {
	m.activeStep = todoWorkspacePickerStep
	m.selectedWorkspace = nil
	m.selectedDirPath = ""
	m.focusIndex = 0
	m.nameErr = ""
	m.nameInput.SetValue("")
	m.descInput.SetValue("")
	return func() tea.Msg { return requestWorkspacesStateMsg{} }
}

func (m TodoFormModel) Keys() help.KeyMap {
	switch m.activeStep {
	case todoWorkspacePickerStep:
		return workspacePickerKeys
	case todoFilePickerStep:
		return filePickerKeys
	case todoNameInputStep:
		return todoFormInputKeys
	default:
		return formKeys
	}
}

func (m TodoFormModel) Update(msg tea.Msg) (TodoFormModel, tea.Cmd) {
	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		m.SetSize(wsm.Width, wsm.Height)
		return m, nil
	}
	if msg, ok := msg.(workspacesStateUpdatedMsg); ok {
		m.activeWorkspaceID = msg.activeWorkspaceID
		var cmd tea.Cmd
		m.workspacePicker, cmd = m.workspacePicker.Update(msg)
		return m, cmd
	}
	if _, ok := msg.(todoCreatedMsg); ok {
		return m, func() tea.Msg { return todoFormCancelledMsg{} }
	}
	if msg, ok := msg.(errMsg); ok {
		m.nameErr = msg.err.Error()
		return m, nil
	}

	switch m.activeStep {
	case todoWorkspacePickerStep:
		return m.updateWorkspacePicker(msg)
	case todoFilePickerStep:
		return m.updateFilePicker(msg)
	case todoDirTypeChoiceStep:
		return m.updateDirTypeChoice(msg)
	case todoNameInputStep:
		return m.updateNameInput(msg)
	}
	return m, nil
}

func (m TodoFormModel) updateWorkspacePicker(msg tea.Msg) (TodoFormModel, tea.Cmd) {
	switch msg := msg.(type) {
	case workspaceSelectedMsg:
		ws := msg.workspace
		m.selectedWorkspace = &ws
		m.activeStep = todoNameInputStep
		m.focusIndex = 0
		m.nameInput.Focus()
		m.descInput.Blur()
		m.nameInput.SetValue("")
		m.descInput.SetValue("")
		m.nameErr = ""
		return m, textinput.Blink
	case addDirectoryRequestMsg:
		m.filePicker = NewFilePickerModel()
		m.filePicker.SetSize(m.width, m.height)
		m.activeStep = todoFilePickerStep
		return m, m.filePicker.Init()
	case tea.KeyMsg:
		if key.Matches(msg, workspacePickerKeys.Back) {
			return m, func() tea.Msg { return todoFormCancelledMsg{} }
		}
	}

	var cmd tea.Cmd
	m.workspacePicker, cmd = m.workspacePicker.Update(msg)
	return m, cmd
}

func (m TodoFormModel) updateFilePicker(msg tea.Msg) (TodoFormModel, tea.Cmd) {
	switch msg := msg.(type) {
	case directorySelectedMsg:
		m.selectedDirPath = msg.path
		m.activeStep = todoDirTypeChoiceStep
		return m, nil
	case tea.KeyMsg:
		if key.Matches(msg, filePickerKeys.Back) {
			m.activeStep = todoWorkspacePickerStep
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.filePicker, cmd = m.filePicker.Update(msg)
	return m, cmd
}

func (m TodoFormModel) updateDirTypeChoice(msg tea.Msg) (TodoFormModel, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		path := m.selectedDirPath
		switch msg.String() {
		case "w":
			m.activeStep = todoWorkspacePickerStep
			return m, func() tea.Msg {
				return addWorkspaceIntentMsg{path: path, asRoot: false}
			}
		case "r":
			m.activeStep = todoWorkspacePickerStep
			return m, func() tea.Msg {
				return addWorkspaceIntentMsg{path: path, asRoot: true}
			}
		case "esc":
			m.activeStep = todoFilePickerStep
			return m, nil
		}
	}
	return m, nil
}

func (m TodoFormModel) updateNameInput(msg tea.Msg) (TodoFormModel, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(msg, todoFormInputKeys.Submit):
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
			return m, func() tea.Msg {
				return createTodoIntentMsg{
					name:        name,
					description: desc,
					workspaceID: wsID,
				}
			}
		case key.Matches(msg, todoFormInputKeys.Back):
			m.activeStep = todoWorkspacePickerStep
			return m, func() tea.Msg { return requestWorkspacesStateMsg{} }
		case key.Matches(msg, todoFormInputKeys.NextTab):
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

func (m TodoFormModel) View() string {
	switch m.activeStep {
	case todoFilePickerStep:
		return m.filePicker.View()
	case todoDirTypeChoiceStep:
		return todoFormPromptStyle.Render("Add: ") + todoFormPathStyle.Render(abbreviatePath(m.selectedDirPath)) +
			"\n\n" +
			"  w  add as workspace\n" +
			"  r  add as root\n\n" +
			"  esc: back"
	case todoNameInputStep:
		wsName := ""
		if m.selectedWorkspace != nil {
			wsName = m.selectedWorkspace.Name
		}
		view := todoFormPromptStyle.Render("Workspace: ") + wsName + "\n\n"
		view += m.nameInput.View() + "\n"
		view += m.descInput.View() + "\n"
		if m.nameErr != "" {
			view += todoFormErrStyle.Render(m.nameErr)
		}
		return view
	default:
		return m.workspacePicker.View()
	}
}
