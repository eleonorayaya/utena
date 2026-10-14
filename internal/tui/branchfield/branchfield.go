package branchfield

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
	"github.com/eleonorayaya/utena/internal/workspace"
)

var ModeOptions = []string{"new branch", "use existing"}

func NewModeChoice() ui.Choice {
	return ui.Choice{Options: ModeOptions}
}

func IsNewBranch(c ui.Choice) bool {
	return c.Selected == 0
}

func ResolveDefault(defaultBranch, current string, local []string) string {
	switch {
	case defaultBranch != "":
		return defaultBranch
	case current != "":
		return current
	case len(local) > 0:
		return local[0]
	default:
		return "main"
	}
}

type Row struct {
	Workspace     workspace.Workspace
	Branch        string
	Picked        bool
	Loading       bool
	Err           string
	Local         []string
	Current       string
	DefaultBranch string
	Editing       bool
	Checking      string
	input         textinput.Model
}

func NewRow(ws workspace.Workspace) Row {
	return Row{Workspace: ws, Loading: true}
}

func (r Row) Busy() bool {
	return r.Loading || r.Editing || r.Checking != ""
}

func (r Row) Loaded(msg provider.BranchesLoadedMsg) Row {
	r.Loading = false
	if msg.Err != nil {
		r.Err = msg.Err.Error()
		if !r.Picked {
			r.Branch = ResolveDefault("", "", nil)
		}
		return r
	}
	r.Err = ""
	r.Local = msg.Branches
	r.Current = msg.CurrentBranch
	r.DefaultBranch = msg.DefaultBranch
	if !r.Picked {
		r.Branch = ResolveDefault(msg.DefaultBranch, msg.CurrentBranch, msg.Branches)
	}
	return r
}

func (r Row) Pick(branch string) Row {
	r.Branch = branch
	r.Picked = true
	r.Err = ""
	return r
}

func (r Row) IsDefault() bool {
	return r.Branch == ResolveDefault(r.DefaultBranch, r.Current, r.Local)
}

func (r Row) StartManual() (Row, tea.Cmd) {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "branch name"
	ti.Width = 40
	r.input = ti
	r.Editing = true
	r.Err = ""
	return r, r.input.Focus()
}

func (r Row) UpdateManual(msg tea.KeyMsg) (Row, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		r.Editing = false
		r.Checking = ""
		r.Err = ""
		return r, nil
	case tea.KeyEnter:
		name := strings.TrimSpace(r.input.Value())
		if name == "" {
			r.Err = "branch name required"
			return r, nil
		}
		r.Err = ""
		r.Checking = name
		return r, provider.CheckBranchExists(r.Workspace.ID, name)
	}
	var cmd tea.Cmd
	r.input, cmd = r.input.Update(msg)
	return r, cmd
}

func (r Row) Checked(msg provider.BranchExistsCheckedMsg) Row {
	if msg.WorkspaceID != r.Workspace.ID || r.Checking == "" || msg.Name != r.Checking {
		return r
	}
	r.Checking = ""
	switch {
	case msg.Err != nil:
		r.Err = msg.Err.Error()
	case !msg.ExistsLocal && !msg.ExistsRemote:
		r.Err = "branch not found: " + msg.Name
	default:
		r.Editing = false
		r = r.Pick(msg.Name)
	}
	return r
}

func fg(c lipgloss.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(c)
}

func (r Row) branchText() string {
	switch {
	case r.Editing && r.Checking != "":
		return r.input.View() + fg(theme.Current.TextMuted).Render("  checking…")
	case r.Editing:
		return r.input.View()
	case r.Loading:
		return fg(theme.Current.TextMuted).Render("…")
	case r.IsDefault():
		return fg(theme.Current.TextMuted).Render(r.Branch)
	default:
		return fg(theme.Current.TextEmphasis).Render(r.Branch)
	}
}

func (r Row) View(nameW int, active bool) string {
	nameStyle := fg(theme.Current.Text)
	if active {
		nameStyle = fg(theme.Current.Primary).Bold(true)
	}
	name := ui.Truncate(r.Workspace.Name, nameW)
	return nameStyle.Render(name) + strings.Repeat(" ", max(nameW-lipgloss.Width(name), 0)+2) + r.BranchView()
}

func (r Row) BranchView() string {
	v := r.branchText()
	if r.Err != "" {
		v += "\n" + fg(theme.Current.Error).Render("✕ "+r.Err)
	}
	return v
}

func RowsView(rows []Row, cursor int, focused bool, maxNameW int) string {
	nameW := 0
	for _, r := range rows {
		nameW = max(nameW, lipgloss.Width(r.Workspace.Name))
	}
	nameW = min(nameW, maxNameW)
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = r.View(nameW, focused && i == cursor)
	}
	return strings.Join(lines, "\n")
}
