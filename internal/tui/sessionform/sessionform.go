package sessionform

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/branchpicker"
	"github.com/eleonorayaya/utena/internal/tui/filepicker"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/sessionprogress"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/workspacepicker"
	"github.com/eleonorayaya/utena/internal/workspace"
)

func errStyle() lipgloss.Style    { return lipgloss.NewStyle().Foreground(theme.Current.Error) }
func promptStyle() lipgloss.Style { return lipgloss.NewStyle().Bold(true) }
func pathStyle() lipgloss.Style   { return lipgloss.NewStyle().Foreground(theme.Current.Path) }

type step int

const (
	workspacePickerStep step = iota
	filePickerStep
	dirTypeChoiceStep
	branchPickerStep
	branchManualInputStep
	branchModeStep
	nameInputStep
)

type Model struct {
	activeStep         step
	workspacePicker    workspacepicker.Model
	filePicker         filepicker.Model
	branchPicker       branchpicker.Model
	nameInput          textinput.Model
	manualBranchInput  textinput.Model
	selectedWorkspace  workspace.Workspace
	selectedBranch     string
	selectedDirPath    string
	nameErr            string
	manualBranchErr    string
	pendingBranchCheck string
	width, height      int
}

func New() Model {
	return Model{
		activeStep:        workspacePickerStep,
		workspacePicker:   workspacepicker.New("Select workspace", false),
		branchPicker:      branchpicker.New(),
		nameInput:         textinput.New(),
		manualBranchInput: textinput.New(),
	}
}

func (m Model) Init() (Model, tea.Cmd) {
	m.activeStep = workspacePickerStep
	m.selectedWorkspace = workspace.Workspace{}
	m.selectedBranch = ""
	m.selectedDirPath = ""
	m.nameErr = ""
	m.nameInput.SetValue("")
	m.manualBranchErr = ""
	m.manualBranchInput.SetValue("")
	m.pendingBranchCheck = ""
	return m, provider.FetchWorkspaces()
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.workspacePicker.SetSize(width, height)
	m.filePicker.SetSize(width, height)
	m.branchPicker.SetSize(width, height)
}

func (m Model) Keys() help.KeyMap {
	switch m.activeStep {
	case workspacePickerStep:
		return mergedKeyMap{keymaps: []help.KeyMap{formKeys, workspacepicker.Keys}}
	case filePickerStep:
		return mergedKeyMap{keymaps: []help.KeyMap{formKeys, filepicker.Keys}}
	case branchPickerStep:
		return mergedKeyMap{keymaps: []help.KeyMap{formKeys, branchpicker.Keys}}
	case dirTypeChoiceStep, branchModeStep, branchManualInputStep:
		return formKeys
	default:
		return formKeys
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
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
	case provider.SessionCreatedMsg:
		return m, tea.Sequence(
			router.NavigateTo(router.SessionProgressView),
			sessionprogress.Start(msg.ID),
		)
	case provider.BranchExistsCheckedMsg:
		return m.onBranchExistsChecked(msg), nil
	}

	switch m.activeStep {
	case workspacePickerStep:
		return m.updateWorkspacePicker(msg)
	case filePickerStep:
		return m.updateFilePicker(msg)
	case dirTypeChoiceStep:
		return m.updateDirTypeChoice(msg)
	case branchPickerStep:
		return m.updateBranchPicker(msg)
	case branchManualInputStep:
		return m.updateBranchManualInput(msg)
	case branchModeStep:
		return m.updateBranchMode(msg)
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
		m.selectedWorkspace = msg.Workspace
		if msg.Workspace.IsGitRepo {
			m.activeStep = branchPickerStep
			m.branchPicker = branchpicker.New()
			m.branchPicker.SetSize(m.width, m.height)
			return m, provider.RequestBranches(msg.Workspace.ID)
		}
		m.activeStep = nameInputStep
		m.initNameInput()
		return m, m.nameInput.Focus()

	case workspacepicker.AddDirectoryMsg:
		m.activeStep = filePickerStep
		m.filePicker = filepicker.New()
		m.filePicker.SetSize(m.width, m.height)
		var cmd tea.Cmd
		m.filePicker, cmd = m.filePicker.Init()
		return m, cmd

	case tea.KeyMsg:
		if key.Matches(msg, formKeys.Back) {
			return m, router.Back()
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

func (m Model) updateBranchPicker(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case branchpicker.SelectedMsg:
		m.selectedBranch = msg.Branch
		m.activeStep = branchModeStep
		return m, nil

	case branchpicker.FetchRequestedMsg:
		var cmd tea.Cmd
		m.branchPicker, cmd = m.branchPicker.Update(msg)
		return m, tea.Batch(cmd, provider.FetchOriginBranches(m.selectedWorkspace.ID))

	case branchpicker.ManualEntryRequestedMsg:
		m.initManualBranchInput()
		m.activeStep = branchManualInputStep
		return m, m.manualBranchInput.Focus()

	case provider.BranchesFetchedMsg:
		var cmd tea.Cmd
		m.branchPicker, cmd = m.branchPicker.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		if key.Matches(msg, formKeys.Back) {
			m.activeStep = workspacePickerStep
			return m, provider.RequestWorkspacesState()
		}
	}

	var cmd tea.Cmd
	m.branchPicker, cmd = m.branchPicker.Update(msg)
	return m, cmd
}

func (m Model) updateBranchManualInput(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, formKeys.Submit):
			name := strings.TrimSpace(m.manualBranchInput.Value())
			if name == "" {
				m.manualBranchErr = "branch name required"
				return m, nil
			}
			m.manualBranchErr = ""
			m.pendingBranchCheck = name
			return m, provider.CheckBranchExists(m.selectedWorkspace.ID, name)
		case key.Matches(msg, formKeys.Back):
			m.activeStep = branchPickerStep
			m.manualBranchErr = ""
			m.pendingBranchCheck = ""
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.manualBranchInput, cmd = m.manualBranchInput.Update(msg)
	return m, cmd
}

func (m Model) onBranchExistsChecked(msg provider.BranchExistsCheckedMsg) Model {
	if m.pendingBranchCheck == "" || msg.Name != m.pendingBranchCheck {
		return m
	}
	m.pendingBranchCheck = ""
	if msg.Err != nil {
		m.manualBranchErr = msg.Err.Error()
		m.activeStep = branchManualInputStep
		return m
	}
	if !msg.ExistsLocal && !msg.ExistsRemote {
		m.manualBranchErr = "branch not found: " + msg.Name
		m.activeStep = branchManualInputStep
		return m
	}
	m.manualBranchErr = ""
	m.selectedBranch = msg.Name
	m.activeStep = branchModeStep
	return m
}

func (m Model) updateBranchMode(msg tea.Msg) (Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "n":
			m.activeStep = nameInputStep
			m.initNameInput()
			return m, m.nameInput.Focus()
		case "e":
			return m, provider.CreateSession("", m.selectedWorkspace.ID, m.selectedBranch, "", m.selectedWorkspace.Path)
		case "esc":
			m.activeStep = branchPickerStep
			return m, nil
		}
	}
	return m, nil
}

func (m Model) updateNameInput(msg tea.Msg) (Model, tea.Cmd) {
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
			return m, provider.CreateSession(name, m.selectedWorkspace.ID, "", m.selectedBranch, m.selectedWorkspace.Path)
		case key.Matches(msg, formKeys.Back):
			if m.selectedWorkspace.IsGitRepo {
				m.activeStep = branchModeStep
				m.nameErr = ""
				return m, nil
			}
			m.activeStep = workspacePickerStep
			m.nameErr = ""
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return m, cmd
}

func (m *Model) initNameInput() {
	ti := textinput.New()
	ti.Prompt = "Session name: "
	ti.Placeholder = defaultSessionName(m.selectedWorkspace.Name)
	m.nameInput = ti
	m.nameErr = ""
}

func (m *Model) initManualBranchInput() {
	ti := textinput.New()
	ti.Prompt = "Branch name: "
	m.manualBranchInput = ti
	m.manualBranchErr = ""
	m.pendingBranchCheck = ""
}

func (m Model) View() string {
	switch m.activeStep {
	case filePickerStep:
		return m.filePicker.View()
	case dirTypeChoiceStep:
		return promptStyle().Render("Add: ") + pathStyle().Render(workspacepicker.AbbreviatePath(m.selectedDirPath)) +
			"\n\n" +
			"  w  add as workspace\n" +
			"  r  add as root (scan subdirectories)\n\n" +
			"  esc: back"
	case branchPickerStep:
		return m.branchPicker.View()
	case branchManualInputStep:
		var b strings.Builder
		b.WriteString("Enter a branch name (local or remote)\n\n")
		b.WriteString(m.manualBranchInput.View())
		if m.pendingBranchCheck != "" {
			b.WriteString("\n\nchecking…")
		}
		if m.manualBranchErr != "" {
			b.WriteString("\n" + errStyle().Render(m.manualBranchErr))
		}
		return b.String()
	case branchModeStep:
		return promptStyle().Render("Branch: ") + pathStyle().Render(m.selectedBranch) +
			"\n\n" +
			"  n  New branch from " + pathStyle().Render(m.selectedBranch) + "\n" +
			"  e  Use existing branch " + pathStyle().Render(m.selectedBranch) + "\n\n" +
			"  esc: back"
	case nameInputStep:
		var b strings.Builder
		fmt.Fprintf(&b, "Workspace: %s\n\n", m.selectedWorkspace.Name)
		b.WriteString(m.nameInput.View())
		if m.nameErr != "" {
			b.WriteString("\n" + errStyle().Render(m.nameErr))
		}
		return b.String()
	default:
		return m.workspacePicker.View()
	}
}

func defaultSessionName(workspaceName string) string {
	return strings.ToLower(strings.ReplaceAll(workspaceName, " ", "-"))
}
