package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type createTodoMsg struct {
	name        string
	description string
	workspaceID string
}

type todoCreatedMsg struct{}

type NewTodoModel struct {
	workspace  *workspace.Workspace
	nameInput  textinput.Model
	descInput  textinput.Model
	focusIndex int
	err        string
}

func NewNewTodoModel(ws *workspace.Workspace) NewTodoModel {
	name := textinput.New()
	name.Prompt = "Name: "
	name.Placeholder = "todo name"
	name.Focus()

	desc := textinput.New()
	desc.Prompt = "Description: "
	desc.Placeholder = "optional description"

	return NewTodoModel{
		workspace: ws,
		nameInput: name,
		descInput: desc,
	}
}

func (m NewTodoModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m NewTodoModel) Update(msg tea.Msg) (NewTodoModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, nameInputKeyMap.Confirm):
			name := m.nameInput.Value()
			if name == "" {
				m.err = "name is required"
				return m, nil
			}
			m.err = ""
			workspaceID := ""
			if m.workspace != nil {
				workspaceID = m.workspace.ID
			}
			return m, func() tea.Msg {
				return createTodoMsg{
					name:        name,
					description: m.descInput.Value(),
					workspaceID: workspaceID,
				}
			}
		case key.Matches(msg, nameInputKeyMap.Back):
			return m, func() tea.Msg {
				return switchToTodoListMsg{}
			}
		case msg.String() == "tab" || msg.String() == "shift+tab":
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

	m.err = ""
	var cmd tea.Cmd
	if m.focusIndex == 0 {
		m.nameInput, cmd = m.nameInput.Update(msg)
	} else {
		m.descInput, cmd = m.descInput.Update(msg)
	}
	return m, cmd
}

func (m NewTodoModel) View() string {
	var b strings.Builder

	wsName := "none"
	if m.workspace != nil {
		wsName = m.workspace.Name
	}
	b.WriteString(fmt.Sprintf("Workspace: %s\n\n", wsName))
	b.WriteString(m.nameInput.View())
	b.WriteString("\n")
	b.WriteString(m.descInput.View())
	b.WriteString("\n")

	if m.err != "" {
		b.WriteString(errStyle.Render(m.err))
		b.WriteString("\n")
	}

	return b.String()
}
