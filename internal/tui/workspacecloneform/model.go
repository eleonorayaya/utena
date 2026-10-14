package workspacecloneform

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

const (
	urlField = iota
	dirField
	rootField
)

var labels = [...]string{"URL", "Directory", "Root"}

type Model struct {
	form         ui.Form
	urlInput     textinput.Model
	dirNameInput textinput.Model
	rootsLoaded  bool
	rootsErr     error
	root         ui.Choice
	cloning      bool
	urlErr       string
	status       ui.Status
	width        int
	height       int
}

func New() Model {
	url := ui.NewInput("git@github.com:org/repo.git")
	url.CharLimit = 512

	dirName := ui.NewInput("derived from URL")
	dirName.CharLimit = 128

	return Model{form: ui.NewForm("Clone workspace", 2), urlInput: url, dirNameInput: dirName}
}

func (m Model) Init() (Model, tea.Cmd) {
	m.urlInput.SetValue("")
	m.dirNameInput.SetValue("")
	m.dirNameInput.Blur()
	m.rootsLoaded = false
	m.rootsErr = nil
	m.root = ui.Choice{}
	m.form = ui.NewForm("Clone workspace", 2)
	m.cloning = false
	m.urlErr = ""
	m.status = ui.Status{}
	return m, tea.Batch(m.urlInput.Focus(), provider.FetchWorkspaceRoots())
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.urlInput.Width = ui.InputWidth(width, labels[:]...)
	m.dirNameInput.Width = m.urlInput.Width
}

func (m Model) Keys() help.KeyMap {
	k := keys
	k.Root.SetEnabled(m.form.Focused() == rootField)
	return k
}

func (m Model) Status() ui.Status {
	switch {
	case m.cloning:
		return ui.Status{Text: "cloning…"}
	case m.rootsErr != nil:
		return ui.Status{Text: "failed to load roots: " + m.rootsErr.Error(), Kind: ui.StatusError}
	case m.rootsLoaded && len(m.root.Options) == 0:
		return ui.Status{Text: "no workspace roots configured — add one to ~/.config/utena/config.json first", Kind: ui.StatusError}
	}
	return m.status
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
		m.root = ui.Choice{Options: msg.Roots}
		fields := 2
		if len(msg.Roots) > 1 {
			fields = 3
		}
		focus := m.form.Focused()
		m.form = ui.NewForm("Clone workspace", fields).Focus(focus)
		return m, nil

	case provider.WorkspaceClonedMsg:
		m.cloning = false
		if msg.Err != nil {
			m.urlErr = msg.Err.Error()
			return m, nil
		}
		return m, router.Back()

	case tea.KeyMsg:
		return m.onKey(msg)
	}

	return m.routeInput(msg)
}

func (m Model) onKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if key.Matches(msg, keys.Back) {
		return m, router.Back()
	}
	if m.cloning {
		return m, nil
	}
	switch {
	case key.Matches(msg, keys.Submit):
		return m.submit()
	case key.Matches(msg, keys.NextField):
		return m.setFocus(m.form.Focused() + 1)
	case key.Matches(msg, keys.PrevField):
		return m.setFocus(m.form.Focused() - 1)
	}
	if m.form.Focused() == rootField {
		m.root, _ = m.root.Update(msg)
		return m, nil
	}
	return m.routeInput(msg)
}

func (m Model) routeInput(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.form.Focused() {
	case urlField:
		m.urlInput, cmd = m.urlInput.Update(msg)
		if _, ok := msg.(tea.KeyMsg); ok {
			m.urlErr = ""
		}
	case dirField:
		m.dirNameInput, cmd = m.dirNameInput.Update(msg)
	}
	return m, cmd
}

func (m Model) setFocus(f int) (Model, tea.Cmd) {
	m.form = m.form.Focus(f)
	m.urlInput.Blur()
	m.dirNameInput.Blur()
	switch m.form.Focused() {
	case urlField:
		return m, m.urlInput.Focus()
	case dirField:
		return m, m.dirNameInput.Focus()
	}
	return m, nil
}

func (m Model) submit() (Model, tea.Cmd) {
	url := strings.TrimSpace(m.urlInput.Value())
	if url == "" {
		m.urlErr = "clone URL is required"
		return m, nil
	}
	if !m.rootsLoaded {
		m.status = ui.Status{Text: "still loading workspace roots…", Kind: ui.StatusWarning}
		return m, nil
	}
	if len(m.root.Options) == 0 {
		return m, nil
	}
	dirName := strings.TrimSpace(m.dirNameInput.Value())
	m.urlErr = ""
	m.status = ui.Status{}
	m.cloning = true
	return m, provider.CloneWorkspace(url, m.root.Value(), dirName)
}

func (m Model) View() string {
	fields := []ui.Field{
		{Label: labels[urlField], Value: m.urlInput.View(), Error: m.urlErr},
		{Label: labels[dirField], Value: m.dirNameInput.View()},
	}
	if len(m.root.Options) > 1 {
		fields = append(fields, ui.Field{Label: labels[rootField], Value: m.root.View(m.form.Focused() == rootField)})
	}
	out := m.form.View(fields, m.width)
	if root := m.root.Value(); root != "" {
		indent := strings.Repeat(" ", ui.GutterWidth+ui.LabelWidth(fields))
		out += "\n\n" + ui.Clamp(indent+ui.Muted().Render("→ "+m.targetPreview(root)), m.width)
	}
	return ui.FitHeight(out, m.height)
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

func (m Model) CapturesText() bool {
	return m.form.Focused() != rootField
}
