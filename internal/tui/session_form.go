package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/workspace"
)

var (
	sfErrStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	sfPromptStyle = lipgloss.NewStyle().Bold(true)
	sfPathStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
)

type sessionFormStep int

const (
	sessionWorkspacePickerStep sessionFormStep = iota
	sessionFilePickerStep
	sessionDirTypeChoiceStep
	sessionBranchPickerStep
	sessionNameInputStep
)

type SessionFormModel struct {
	activeStep        sessionFormStep
	workspacePicker   WorkspacePickerModel
	filePicker        FilePickerModel
	branchPicker      BranchPickerModel
	nameInput         textinput.Model
	selectedWorkspace workspace.Workspace
	selectedBranch    string
	selectedDirPath   string
	nameErr           string
	width, height     int
}

func NewSessionFormModel() SessionFormModel {
	return SessionFormModel{
		activeStep:      sessionWorkspacePickerStep,
		workspacePicker: NewWorkspacePickerModel("Select workspace", false),
		branchPicker:    NewBranchPickerModel(),
		nameInput:       textinput.New(),
	}
}

func (m SessionFormModel) Init() (SessionFormModel, tea.Cmd) {
	m.activeStep = sessionWorkspacePickerStep
	m.selectedWorkspace = workspace.Workspace{}
	m.selectedBranch = ""
	m.selectedDirPath = ""
	m.nameErr = ""
	m.nameInput.SetValue("")
	return m, provider.FetchWorkspaces()
}

func (m *SessionFormModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.workspacePicker.SetSize(width, height)
	m.filePicker.SetSize(width, height)
	m.branchPicker.SetSize(width, height)
}

func (m SessionFormModel) Keys() help.KeyMap {
	switch m.activeStep {
	case sessionWorkspacePickerStep:
		return mergedKeyMap{keymaps: []help.KeyMap{formKeys, workspacePickerKeys}}
	case sessionFilePickerStep:
		return mergedKeyMap{keymaps: []help.KeyMap{formKeys, filePickerKeys}}
	case sessionBranchPickerStep:
		return mergedKeyMap{keymaps: []help.KeyMap{formKeys, branchPickerKeys}}
	case sessionDirTypeChoiceStep:
		return formKeys
	default:
		return formKeys
	}
}

func (m SessionFormModel) Update(msg tea.Msg) (SessionFormModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.OnWindowSizeMsg(msg)
	case provider.WorkspacesStateUpdatedMsg:
		var cmd tea.Cmd
		m.workspacePicker, cmd = m.workspacePicker.Update(msg)
		return m, cmd
	case provider.ErrMsg:
		m.nameErr = msg.Err.Error()
		return m, nil
	}

	switch m.activeStep {
	case sessionWorkspacePickerStep:
		return m.updateWorkspacePicker(msg)
	case sessionFilePickerStep:
		return m.updateFilePicker(msg)
	case sessionDirTypeChoiceStep:
		return m.updateDirTypeChoice(msg)
	case sessionBranchPickerStep:
		return m.updateBranchPicker(msg)
	case sessionNameInputStep:
		return m.updateNameInput(msg)
	}
	return m, nil
}

func (m SessionFormModel) OnWindowSizeMsg(msg tea.WindowSizeMsg) (SessionFormModel, tea.Cmd) {
	m.SetSize(msg.Width, msg.Height)
	return m, nil
}

func (m SessionFormModel) OnKeyMsg(_ tea.KeyMsg) (SessionFormModel, tea.Cmd, bool) {
	return m, nil, false
}

func (m SessionFormModel) updateWorkspacePicker(msg tea.Msg) (SessionFormModel, tea.Cmd) {
	switch msg := msg.(type) {
	case workspaceSelectedMsg:
		m.selectedWorkspace = msg.workspace
		if msg.workspace.IsGitRepo {
			m.activeStep = sessionBranchPickerStep
			m.branchPicker = NewBranchPickerModel()
			m.branchPicker.SetSize(m.width, m.height)
			return m, provider.RequestBranches(msg.workspace.ID)
		}
		m.activeStep = sessionNameInputStep
		m.initNameInput()
		return m, m.nameInput.Focus()

	case addDirectoryRequestMsg:
		m.activeStep = sessionFilePickerStep
		m.filePicker = NewFilePickerModel()
		m.filePicker.SetSize(m.width, m.height)
		var cmd tea.Cmd
		m.filePicker, cmd = m.filePicker.Init()
		return m, cmd

	case tea.KeyMsg:
		if key.Matches(msg, formKeys.Back) {
			return m, func() tea.Msg { return navigateMsg{target: sessionListView} }
		}
	}

	var cmd tea.Cmd
	m.workspacePicker, cmd = m.workspacePicker.Update(msg)
	return m, cmd
}

func (m SessionFormModel) updateFilePicker(msg tea.Msg) (SessionFormModel, tea.Cmd) {
	switch msg := msg.(type) {
	case directorySelectedMsg:
		m.selectedDirPath = msg.path
		m.activeStep = sessionDirTypeChoiceStep
		return m, nil

	case tea.KeyMsg:
		if key.Matches(msg, formKeys.Back) {
			m.activeStep = sessionWorkspacePickerStep
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.filePicker, cmd = m.filePicker.Update(msg)
	return m, cmd
}

func (m SessionFormModel) updateDirTypeChoice(msg tea.Msg) (SessionFormModel, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "w":
			m.activeStep = sessionWorkspacePickerStep
			return m, provider.AddWorkspace(m.selectedDirPath, false)
		case "r":
			m.activeStep = sessionWorkspacePickerStep
			return m, provider.AddWorkspace(m.selectedDirPath, true)
		case "esc":
			m.activeStep = sessionFilePickerStep
			return m, nil
		}
	}
	return m, nil
}

func (m SessionFormModel) updateBranchPicker(msg tea.Msg) (SessionFormModel, tea.Cmd) {
	switch msg := msg.(type) {
	case branchSelectedMsg:
		m.selectedBranch = msg.branch
		m.activeStep = sessionNameInputStep
		m.initNameInput()
		return m, m.nameInput.Focus()

	case tea.KeyMsg:
		if key.Matches(msg, formKeys.Back) {
			m.activeStep = sessionWorkspacePickerStep
			return m, provider.RequestWorkspacesState()
		}
	}

	var cmd tea.Cmd
	m.branchPicker, cmd = m.branchPicker.Update(msg)
	return m, cmd
}

func (m SessionFormModel) updateNameInput(msg tea.Msg) (SessionFormModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, formKeys.Submit):
			name := m.nameInput.Value()
			if name == "" {
				name = m.nameInput.Placeholder
			}
			if err := session.ValidateSessionName(name); err != nil {
				m.nameErr = err.Error()
				return m, nil
			}
			return m, provider.CreateSession(name, m.selectedWorkspace.ID, m.selectedBranch, m.selectedWorkspace.Path)
		case key.Matches(msg, formKeys.Back):
			if m.selectedWorkspace.IsGitRepo {
				m.activeStep = sessionBranchPickerStep
				m.nameErr = ""
				return m, nil
			}
			m.activeStep = sessionWorkspacePickerStep
			m.nameErr = ""
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return m, cmd
}

func (m *SessionFormModel) initNameInput() {
	ti := textinput.New()
	ti.Prompt = "Session name: "
	ti.Placeholder = defaultSessionName(m.selectedWorkspace.Name)
	m.nameInput = ti
	m.nameErr = ""
}

func (m SessionFormModel) View() string {
	switch m.activeStep {
	case sessionFilePickerStep:
		return m.filePicker.View()
	case sessionDirTypeChoiceStep:
		return sfPromptStyle.Render("Add: ") + sfPathStyle.Render(abbreviatePath(m.selectedDirPath)) +
			"\n\n" +
			"  w  add as workspace\n" +
			"  r  add as root (scan subdirectories)\n\n" +
			"  esc: back"
	case sessionBranchPickerStep:
		return m.branchPicker.View()
	case sessionNameInputStep:
		var b strings.Builder
		fmt.Fprintf(&b, "Workspace: %s\n\n", m.selectedWorkspace.Name)
		b.WriteString(m.nameInput.View())
		if m.nameErr != "" {
			b.WriteString("\n" + sfErrStyle.Render(m.nameErr))
		}
		return b.String()
	default:
		return m.workspacePicker.View()
	}
}

func defaultSessionName(workspaceName string) string {
	return strings.ToLower(strings.ReplaceAll(workspaceName, " ", "-"))
}
