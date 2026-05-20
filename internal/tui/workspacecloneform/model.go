package workspacecloneform

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

func titleStyle() lipgloss.Style { return lipgloss.NewStyle().Bold(true) }
func errStyle() lipgloss.Style   { return lipgloss.NewStyle().Foreground(theme.Current.Error) }
func pathStyle() lipgloss.Style  { return lipgloss.NewStyle().Foreground(theme.Current.Path) }
func pendingStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.StatusPending)
}
func helpStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(theme.Current.StatusPending) }

type focusTarget int

const (
	focusURL focusTarget = iota
	focusDirName
)

type Model struct {
	urlInput     textinput.Model
	dirNameInput textinput.Model
	roots        []string
	rootsLoaded  bool
	rootsErr     error
	rootIndex    int
	focus        focusTarget
	cloning      bool
	errMsg       string
	width        int
	height       int
}

func New() Model {
	url := textinput.New()
	url.Prompt = "Clone URL: "
	url.Placeholder = "git@github.com:org/repo.git"
	url.CharLimit = 512

	dirName := textinput.New()
	dirName.Prompt = "Directory name (optional): "
	dirName.Placeholder = "derived from URL"
	dirName.CharLimit = 128

	return Model{urlInput: url, dirNameInput: dirName}
}

func (m Model) Init() (Model, tea.Cmd) {
	m.urlInput.SetValue("")
	m.dirNameInput.SetValue("")
	m.roots = nil
	m.rootsLoaded = false
	m.rootsErr = nil
	m.rootIndex = 0
	m.focus = focusURL
	m.cloning = false
	m.errMsg = ""
	return m, tea.Batch(m.urlInput.Focus(), provider.FetchWorkspaceRoots())
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m Model) Keys() help.KeyMap {
	return keys
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil

	case provider.WorkspaceRootsMsg:
		m.rootsLoaded = true
		if msg.Err != nil {
			m.rootsErr = msg.Err
			return m, nil
		}
		m.roots = msg.Roots
		if m.rootIndex >= len(m.roots) {
			m.rootIndex = 0
		}
		return m, nil

	case provider.WorkspaceClonedMsg:
		m.cloning = false
		if msg.Err != nil {
			m.errMsg = msg.Err.Error()
			return m, nil
		}
		return m, router.Back()

	case tea.KeyMsg:
		return m.onKey(msg)
	}

	return m.routeInput(msg)
}

func (m Model) onKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.cloning {
		return m, nil
	}
	switch {
	case key.Matches(msg, keys.Back):
		return m, router.Back()
	case key.Matches(msg, keys.Submit):
		return m.submit()
	case key.Matches(msg, keys.NextField), key.Matches(msg, keys.PrevField):
		var cmd tea.Cmd
		m, cmd = m.toggleFocus()
		return m, cmd
	case key.Matches(msg, keys.NextRoot):
		if len(m.roots) > 1 {
			m.rootIndex = (m.rootIndex + 1) % len(m.roots)
		}
		return m, nil
	case key.Matches(msg, keys.PrevRoot):
		if len(m.roots) > 1 {
			m.rootIndex = (m.rootIndex - 1 + len(m.roots)) % len(m.roots)
		}
		return m, nil
	}
	return m.routeInput(msg)
}

func (m Model) routeInput(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focus {
	case focusURL:
		m.urlInput, cmd = m.urlInput.Update(msg)
	case focusDirName:
		m.dirNameInput, cmd = m.dirNameInput.Update(msg)
	}
	return m, cmd
}

func (m Model) toggleFocus() (Model, tea.Cmd) {
	if m.focus == focusURL {
		m.focus = focusDirName
		m.urlInput.Blur()
		return m, m.dirNameInput.Focus()
	}
	m.focus = focusURL
	m.dirNameInput.Blur()
	return m, m.urlInput.Focus()
}

func (m Model) submit() (Model, tea.Cmd) {
	url := strings.TrimSpace(m.urlInput.Value())
	if url == "" {
		m.errMsg = "clone URL is required"
		return m, nil
	}
	if !m.rootsLoaded {
		m.errMsg = "still loading workspace roots…"
		return m, nil
	}
	if len(m.roots) == 0 {
		m.errMsg = "no workspace roots configured — add one to ~/.config/utena/config.json first"
		return m, nil
	}
	rootPath := m.roots[m.rootIndex]
	dirName := strings.TrimSpace(m.dirNameInput.Value())
	m.errMsg = ""
	m.cloning = true
	return m, provider.CloneWorkspace(url, rootPath, dirName)
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle().Render("Clone workspace from URL"))
	b.WriteString("\n\n")
	b.WriteString(m.urlInput.View())
	b.WriteString("\n")
	b.WriteString(m.dirNameInput.View())
	b.WriteString("\n\n")

	switch {
	case !m.rootsLoaded:
		b.WriteString(pendingStyle().Render("Loading workspace roots…"))
	case m.rootsErr != nil:
		b.WriteString(errStyle().Render("Failed to load roots: " + m.rootsErr.Error()))
	case len(m.roots) == 0:
		b.WriteString(errStyle().Render("No workspace roots configured — add one to ~/.config/utena/config.json first"))
	case len(m.roots) == 1:
		b.WriteString("Target root: " + pathStyle().Render(m.roots[0]))
		b.WriteString("\nWill clone into: " + pathStyle().Render(m.targetPreview(m.roots[0])))
	default:
		fmt.Fprintf(&b, "Target root (ctrl+n / ctrl+p to switch, %d/%d): ", m.rootIndex+1, len(m.roots))
		b.WriteString(pathStyle().Render(m.roots[m.rootIndex]))
		b.WriteString("\nWill clone into: " + pathStyle().Render(m.targetPreview(m.roots[m.rootIndex])))
	}

	if m.cloning {
		b.WriteString("\n\n")
		b.WriteString(pendingStyle().Render("Cloning… this can take a while for large repos."))
	}

	if m.errMsg != "" {
		b.WriteString("\n\n")
		b.WriteString(errStyle().Render(m.errMsg))
	}

	b.WriteString("\n\n")
	b.WriteString(helpStyle().Render("tab/shift+tab switch fields · enter clone · esc back"))
	return b.String()
}

func (m Model) targetPreview(rootPath string) string {
	dirName := strings.TrimSpace(m.dirNameInput.Value())
	if dirName == "" {
		dirName = derivedName(m.urlInput.Value())
	}
	if dirName == "" {
		dirName = "<repo-name>"
	}
	return filepath.Join(rootPath, dirName)
}

func derivedName(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}
	_, repo, err := git.ParseRepoFullName(url)
	if err != nil {
		return ""
	}
	return repo
}
