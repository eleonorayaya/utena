package sessiondetail

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/sessionprogress"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

func titleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(theme.Current.TextEmphasis)
}

func labelStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.TextMuted).Width(12)
}

func valueStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.Text)
}

func warningStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.AccentLavender)
}

func sectionStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.TextMuted).Bold(true)
}

type Model struct {
	sess            *session.Session
	prs             []git.PullRequest
	pendingDeleteID uint
	width           int
	height          int
}

func New() Model {
	return Model{}
}

func (m Model) Init() (Model, tea.Cmd) {
	return m, nil
}

func (m Model) Keys() help.KeyMap {
	return keys
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case SelectMsg:
		s := msg.Session
		m.sess = &s
		m.prs = nil
		m.pendingDeleteID = 0
		if s.WorkspaceID != 0 {
			return m, provider.FetchPRs(s.WorkspaceID, "")
		}
		return m, nil
	case provider.PRsStateUpdatedMsg:
		if m.sess != nil && msg.WorkspaceID == m.sess.WorkspaceID {
			m.prs = filterPRsByBranch(msg.PullRequests, m.sess.GitBranch)
		}
		return m, nil
	case tea.KeyMsg:
		return m.onKeyMsg(msg)
	}
	return m, nil
}

func (m Model) onKeyMsg(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.sess == nil {
		return m, nil
	}

	pendingID := m.pendingDeleteID
	m.pendingDeleteID = 0

	switch {
	case key.Matches(msg, keys.Back):
		return m, router.Back()

	case key.Matches(msg, keys.Activate):
		return m, provider.ActivateSession(m.sess.ID)

	case key.Matches(msg, keys.Archive):
		archivable := m.sess.Status == session.StatusActive ||
			m.sess.Status == session.StatusInactive ||
			m.sess.Status == session.StatusCompleted
		if !archivable {
			return m, nil
		}
		return m, tea.Batch(
			provider.ArchiveSession(m.sess.ID),
			router.Back(),
		)

	case key.Matches(msg, keys.Delete):
		if pendingID == m.sess.ID {
			return m, tea.Batch(
				provider.DeleteSession(m.sess.ID),
				router.Back(),
			)
		}
		m.pendingDeleteID = m.sess.ID
		return m, nil

	case key.Matches(msg, keys.Repair):
		if m.sess.Status != session.StatusBroken {
			return m, nil
		}
		id := m.sess.ID
		return m, tea.Sequence(
			router.NavigateTo(router.SessionProgressView),
			sessionprogress.Start(id),
		)
	}

	return m, nil
}

func filterPRsByBranch(prs []git.PullRequest, branch *git.Branch) []git.PullRequest {
	if branch == nil {
		return nil
	}
	var out []git.PullRequest
	for _, pr := range prs {
		if pr.HeadBranchID != nil && *pr.HeadBranchID == branch.ID {
			out = append(out, pr)
		}
	}
	return out
}

func (m Model) View() string {
	if m.sess == nil {
		return "No session selected"
	}

	var b strings.Builder
	s := m.sess

	b.WriteString(titleStyle().Render(s.Name))
	b.WriteString("\n\n")

	b.WriteString(labelStyle().Render("Status") + valueStyle().Render(string(s.Status)) + "\n")

	if s.Workspace != nil {
		b.WriteString(labelStyle().Render("Workspace") + valueStyle().Render(s.Workspace.Name) + "\n")
	}

	if s.TmuxSession != nil {
		b.WriteString("\n" + sectionStyle().Render("Tmux") + "\n")
		b.WriteString(labelStyle().Render("Session") + valueStyle().Render(s.TmuxSession.Name) + "\n")
		if s.TmuxSession.StartDir != "" {
			b.WriteString(labelStyle().Render("Dir") + valueStyle().Render(s.TmuxSession.StartDir) + "\n")
		}
	}

	if s.GitBranch != nil {
		b.WriteString("\n" + sectionStyle().Render("Git") + "\n")
		b.WriteString(labelStyle().Render("Branch") + valueStyle().Render(s.GitBranch.Name) + "\n")
		for _, pr := range m.prs {
			b.WriteString(labelStyle().Render("PR") + valueStyle().Render(fmt.Sprintf("#%d %s (%s)", pr.Number, pr.Title, pr.State)) + "\n")
		}
	}

	if len(s.ClaudeSessions) > 0 {
		b.WriteString("\n" + sectionStyle().Render("Claude") + "\n")
		for _, cs := range s.ClaudeSessions {
			b.WriteString(labelStyle().Render("Status") + valueStyle().Render(string(cs.Status)) + "\n")
		}
	}

	if s.StatusError != "" {
		b.WriteString("\n" + warningStyle().Render("[!] "+s.StatusError) + "\n")
	}

	return b.String()
}
