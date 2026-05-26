package addworkspaceform

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/branchpicker"
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
	branchPickerStep
	branchModeStep
)

type Model struct {
	activeStep      step
	workspacePicker workspacepicker.Model
	branchPicker    branchpicker.Model
	sessionID       uint
	selectedWS      *workspace.Workspace
	pickedBranch    string
	err             string
	width, height   int
}

func New() Model {
	return Model{
		workspacePicker: workspacepicker.New("Select workspace to add", false),
		branchPicker:    branchpicker.New(""),
	}
}

type StartMsg struct {
	SessionID uint
}

func Start(sessionID uint) tea.Cmd {
	return func() tea.Msg { return StartMsg{SessionID: sessionID} }
}

func (m Model) Init() (Model, tea.Cmd) {
	m.activeStep = workspacePickerStep
	m.sessionID = 0
	m.selectedWS = nil
	m.pickedBranch = ""
	m.err = ""
	return m, provider.FetchWorkspaces()
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.workspacePicker.SetSize(width, height)
	m.branchPicker.SetSize(width, height)
}

func (m Model) Keys() help.KeyMap {
	switch m.activeStep {
	case workspacePickerStep:
		return mergedKeyMap{keymaps: []help.KeyMap{formKeys, workspacepicker.Keys}}
	case branchPickerStep:
		return mergedKeyMap{keymaps: []help.KeyMap{formKeys, branchpicker.Keys}}
	default:
		return formKeys
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil

	case StartMsg:
		m.sessionID = msg.SessionID
		m.activeStep = workspacePickerStep
		m.selectedWS = nil
		m.pickedBranch = ""
		m.err = ""
		return m, provider.RequestWorkspacesState()

	case provider.WorkspacesStateUpdatedMsg:
		var cmd tea.Cmd
		m.workspacePicker, cmd = m.workspacePicker.Update(msg)
		return m, cmd

	case provider.ErrMsg:
		m.err = msg.Err.Error()
		return m, nil

	case provider.WorkspaceAddedToSessionMsg:
		return m, tea.Sequence(
			router.NavigateTo(router.SessionProgressView),
			sessionprogress.Start(msg.SessionID),
		)
	}

	switch m.activeStep {
	case workspacePickerStep:
		return m.updateWorkspacePicker(msg)
	case branchPickerStep:
		return m.updateBranchPicker(msg)
	case branchModeStep:
		return m.updateBranchMode(msg)
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
		if !ws.IsGitRepo {
			m.err = "selected workspace is not a git repository"
			return m, nil
		}
		m.selectedWS = &ws
		m.err = ""
		title := fmt.Sprintf("Select branch for %s", ws.Name)
		m.branchPicker = branchpicker.New(title)
		m.branchPicker.SetSize(m.width, m.height)
		m.activeStep = branchPickerStep
		return m, provider.RequestBranches(ws.ID)

	case tea.KeyMsg:
		if key.Matches(msg, formKeys.Back) {
			return m, router.Back()
		}
	}

	var cmd tea.Cmd
	m.workspacePicker, cmd = m.workspacePicker.Update(msg)
	return m, cmd
}

func (m Model) updateBranchPicker(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case branchpicker.SelectedMsg:
		m.pickedBranch = msg.Branch
		m.activeStep = branchModeStep
		return m, nil

	case branchpicker.FetchRequestedMsg:
		var cmd tea.Cmd
		m.branchPicker, cmd = m.branchPicker.Update(msg)
		if m.selectedWS != nil {
			return m, tea.Batch(cmd, provider.FetchOriginBranches(m.selectedWS.ID))
		}
		return m, cmd

	case provider.BranchesFetchedMsg:
		var cmd tea.Cmd
		m.branchPicker, cmd = m.branchPicker.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		if key.Matches(msg, formKeys.Back) {
			m.activeStep = workspacePickerStep
			m.pickedBranch = ""
			return m, provider.RequestWorkspacesState()
		}
	}

	var cmd tea.Cmd
	m.branchPicker, cmd = m.branchPicker.Update(msg)
	return m, cmd
}

func (m Model) updateBranchMode(msg tea.Msg) (Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "n":
		spec := provider.SessionWorkspaceSpec{WorkspaceID: m.selectedWS.ID, BaseBranch: m.pickedBranch}
		return m, provider.AddWorkspaceToSession(m.sessionID, spec)
	case "e":
		spec := provider.SessionWorkspaceSpec{WorkspaceID: m.selectedWS.ID, Branch: m.pickedBranch}
		return m, provider.AddWorkspaceToSession(m.sessionID, spec)
	case "esc":
		m.activeStep = branchPickerStep
		if m.selectedWS != nil {
			return m, provider.RequestBranches(m.selectedWS.ID)
		}
		return m, nil
	}
	return m, nil
}

func (m Model) View() string {
	switch m.activeStep {
	case branchPickerStep:
		v := m.branchPicker.View()
		if m.err != "" {
			v += "\n" + errStyle().Render(m.err)
		}
		return v

	case branchModeStep:
		var b strings.Builder
		wsName := ""
		if m.selectedWS != nil {
			wsName = m.selectedWS.Name
		}
		b.WriteString(promptStyle().Render("Workspace: ") + pathStyle().Render(wsName) + "\n")
		b.WriteString(promptStyle().Render("Branch: ") + pathStyle().Render(m.pickedBranch) + "\n\n")
		b.WriteString("  n  Create new branch from selected\n")
		b.WriteString("  e  Use existing branch\n\n")
		b.WriteString("  esc: back")
		if m.err != "" {
			b.WriteString("\n" + errStyle().Render(m.err))
		}
		return b.String()

	default:
		v := m.workspacePicker.View()
		if m.err != "" {
			v += "\n" + errStyle().Render(m.err)
		}
		return v
	}
}
