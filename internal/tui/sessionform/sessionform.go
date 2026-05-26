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

func padToHeight(s string, h int) string {
	if h <= 0 {
		return s
	}
	n := strings.Count(s, "\n")
	if n >= h {
		return s
	}
	return s + strings.Repeat("\n", h-n)
}

func errStyle() lipgloss.Style  { return lipgloss.NewStyle().Foreground(theme.Current.Error) }
func pathStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(theme.Current.Path) }
func formTitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(theme.Current.TextEmphasis)
}
func formRuleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.SurfaceVariant)
}
func formSectionStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true)
}
func formLabelStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.TextMuted).Width(14)
}
func formKeyStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true)
}
func formHintStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(theme.Current.Text) }

type step int

const (
	workspacePickerStep step = iota
	filePickerStep
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
	selectedWorkspaces []workspace.Workspace
	pickingForIndex    int
	pickedBranches     []string
	selectedDirPath    string
	nameErr            string
	manualBranchErr    string
	pendingBranchCheck string
	width, height      int
}

func New() Model {
	return Model{
		activeStep:        workspacePickerStep,
		workspacePicker:   workspacepicker.New("Select workspaces", true),
		branchPicker:      branchpicker.New(""),
		nameInput:         textinput.New(),
		manualBranchInput: textinput.New(),
	}
}

func (m Model) Init() (Model, tea.Cmd) {
	m.activeStep = workspacePickerStep
	m.selectedWorkspaces = nil
	m.pickingForIndex = 0
	m.pickedBranches = nil
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
	case branchModeStep, branchManualInputStep:
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
		return m.onBranchExistsChecked(msg)
	}

	switch m.activeStep {
	case workspacePickerStep:
		return m.updateWorkspacePicker(msg)
	case filePickerStep:
		return m.updateFilePicker(msg)
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

func (m Model) currentWorkspace() workspace.Workspace {
	if m.pickingForIndex < 0 || m.pickingForIndex >= len(m.selectedWorkspaces) {
		return workspace.Workspace{}
	}
	return m.selectedWorkspaces[m.pickingForIndex]
}

func (m *Model) startBranchPicking(index int) tea.Cmd {
	m.pickingForIndex = index
	ws := m.selectedWorkspaces[index]
	title := fmt.Sprintf("Select branch for %s (%d/%d)", ws.Name, index+1, len(m.selectedWorkspaces))
	m.branchPicker = branchpicker.New(title)
	m.branchPicker.SetSize(m.width, m.height)
	m.activeStep = branchPickerStep
	return provider.RequestBranches(ws.ID)
}

func (m Model) updateWorkspacePicker(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case workspacepicker.SelectedMsg:
		picked := msg.Workspaces
		if len(picked) == 0 {
			picked = []workspace.Workspace{msg.Workspace}
		}
		for _, ws := range picked {
			if !ws.IsGitRepo {
				m.nameErr = "selected workspace is not a git repository — sessions require a git workspace"
				return m, nil
			}
		}
		m.selectedWorkspaces = picked
		m.pickedBranches = make([]string, len(picked))
		m.nameErr = ""
		return m, m.startBranchPicking(0)

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
		m.activeStep = workspacePickerStep
		return m, provider.AddWorkspace(msg.Path, true)

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

func (m Model) updateBranchPicker(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case branchpicker.SelectedMsg:
		m.pickedBranches[m.pickingForIndex] = msg.Branch
		if m.pickingForIndex+1 < len(m.selectedWorkspaces) {
			return m, m.startBranchPicking(m.pickingForIndex + 1)
		}
		m.activeStep = branchModeStep
		return m, nil

	case branchpicker.FetchRequestedMsg:
		var cmd tea.Cmd
		m.branchPicker, cmd = m.branchPicker.Update(msg)
		return m, tea.Batch(cmd, provider.FetchOriginBranches(m.currentWorkspace().ID))

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
			if m.pickingForIndex == 0 {
				m.activeStep = workspacePickerStep
				m.pickedBranches = nil
				return m, provider.RequestWorkspacesState()
			}
			return m, m.startBranchPicking(m.pickingForIndex - 1)
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
			return m, provider.CheckBranchExists(m.currentWorkspace().ID, name)
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

func (m Model) onBranchExistsChecked(msg provider.BranchExistsCheckedMsg) (Model, tea.Cmd) {
	if m.activeStep != branchManualInputStep || m.pendingBranchCheck == "" || msg.Name != m.pendingBranchCheck {
		return m, nil
	}
	m.pendingBranchCheck = ""
	if msg.Err != nil {
		m.manualBranchErr = msg.Err.Error()
		return m, nil
	}
	if !msg.ExistsLocal && !msg.ExistsRemote {
		m.manualBranchErr = "branch not found: " + msg.Name
		return m, nil
	}
	m.manualBranchErr = ""
	m.pickedBranches[m.pickingForIndex] = msg.Name
	if m.pickingForIndex+1 < len(m.selectedWorkspaces) {
		return m, m.startBranchPicking(m.pickingForIndex + 1)
	}
	m.activeStep = branchModeStep
	return m, nil
}

func (m Model) updateBranchMode(msg tea.Msg) (Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "n":
			m.activeStep = nameInputStep
			m.initNameInput()
			return m, m.nameInput.Focus()
		case "e":
			return m, provider.CreateSession("", buildSpecs(m.selectedWorkspaces, m.pickedBranches, false))
		case "esc":
			return m, m.startBranchPicking(len(m.selectedWorkspaces) - 1)
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
			return m, provider.CreateSession(name, buildSpecs(m.selectedWorkspaces, m.pickedBranches, true))
		case key.Matches(msg, formKeys.Back):
			m.activeStep = branchModeStep
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
	if len(m.selectedWorkspaces) > 0 {
		ti.Placeholder = defaultSessionName(m.selectedWorkspaces[0].Name)
	}
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
		return padToHeight(strings.TrimRight(m.filePicker.View(), "\n"), m.height-1)
	case branchPickerStep:
		return padToHeight(strings.TrimRight(m.branchPicker.View(), "\n"), m.height-1)
	case branchManualInputStep:
		var b strings.Builder
		ws := m.currentWorkspace()
		fmt.Fprintf(&b, "Enter a branch name for %s (local or remote)\n\n", pathStyle().Render(ws.Name))
		b.WriteString(m.manualBranchInput.View())
		if m.pendingBranchCheck != "" {
			b.WriteString("\n\nchecking…")
		}
		if m.manualBranchErr != "" {
			b.WriteString("\n" + errStyle().Render(m.manualBranchErr))
		}
		return padToHeight(b.String(), m.height-1)
	case branchModeStep:
		var b strings.Builder
		ruleW := m.width
		if ruleW < 1 {
			ruleW = 40
		}
		b.WriteString(formTitleStyle().Render("New Session"))
		b.WriteString("\n")
		b.WriteString(formRuleStyle().Render(strings.Repeat("─", ruleW)))
		b.WriteString("\n\n")
		b.WriteString(formSectionStyle().Render("⎇ Branches") + "\n")
		for i, ws := range m.selectedWorkspaces {
			b.WriteString(formLabelStyle().Render(ws.Name) + pathStyle().Render(m.pickedBranches[i]) + "\n")
		}
		b.WriteString("\n" + formSectionStyle().Render("Mode") + "\n")
		b.WriteString(formKeyStyle().Render("n") + "  " + formHintStyle().Render("create new branch from picked") + "\n")
		b.WriteString(formKeyStyle().Render("e") + "  " + formHintStyle().Render("use existing branches as-is") + "\n")
		b.WriteString(formKeyStyle().Render("esc") + "  " + formHintStyle().Render("back") + "\n")
		return padToHeight(b.String(), m.height-1)
	case nameInputStep:
		var b strings.Builder
		ruleW := m.width
		if ruleW < 1 {
			ruleW = 40
		}
		b.WriteString(formTitleStyle().Render("New Session"))
		b.WriteString("\n")
		b.WriteString(formRuleStyle().Render(strings.Repeat("─", ruleW)))
		b.WriteString("\n\n")
		b.WriteString(formSectionStyle().Render("⎇ Branches") + "\n")
		for i, ws := range m.selectedWorkspaces {
			b.WriteString(formLabelStyle().Render(ws.Name) + pathStyle().Render(m.pickedBranches[i]) + "\n")
		}
		b.WriteString("\n")
		b.WriteString(m.nameInput.View())
		if m.nameErr != "" {
			b.WriteString("\n" + errStyle().Render(m.nameErr))
		}
		return padToHeight(b.String(), m.height-1)
	default:
		return padToHeight(strings.TrimRight(m.workspacePicker.View(), "\n"), m.height-1)
	}
}

func defaultSessionName(workspaceName string) string {
	return strings.ToLower(strings.ReplaceAll(workspaceName, " ", "-"))
}

func buildSpecs(wss []workspace.Workspace, branches []string, isNew bool) []provider.SessionWorkspaceSpec {
	out := make([]provider.SessionWorkspaceSpec, len(wss))
	for i, ws := range wss {
		out[i].WorkspaceID = ws.ID
		if isNew {
			out[i].BaseBranch = branches[i]
		} else {
			out[i].Branch = branches[i]
		}
	}
	return out
}
