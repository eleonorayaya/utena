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
	return lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true)
}

func ruleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.SurfaceVariant)
}

func statusColor(status session.SessionStatus) lipgloss.Color {
	switch status {
	case session.StatusActive:
		return theme.Current.StatusReady
	case session.StatusCreating:
		return theme.Current.StatusActive
	case session.StatusBroken:
		return theme.Current.Error
	case session.StatusPending:
		return theme.Current.StatusPending
	default:
		return theme.Current.TextMuted
	}
}

func statusPill(status session.SessionStatus) string {
	s := lipgloss.NewStyle().
		Foreground(theme.Current.TextOnPrimary).
		Background(statusColor(status)).
		Bold(true)
	return s.Render(" " + string(status) + " ")
}

func prStateColor(state git.PRState) lipgloss.Color {
	switch state {
	case git.PRStateOpen:
		return theme.Current.StatusReady
	case git.PRStateDraft:
		return theme.Current.TextMuted
	case git.PRStateClosed:
		return theme.Current.Error
	case git.PRStateMerged:
		return theme.Current.Tertiary
	default:
		return theme.Current.TextMuted
	}
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
		if len(s.Worktrees) > 0 && s.Worktrees[0].Workspace != nil {
			return m, provider.FetchPRs(s.Worktrees[0].Workspace.ID, "")
		}
		return m, nil
	case provider.PRsStateUpdatedMsg:
		if m.sess != nil && len(m.sess.Worktrees) > 0 && m.sess.Worktrees[0].Workspace != nil && msg.WorkspaceID == m.sess.Worktrees[0].Workspace.ID {
			m.prs = filterPRsByBranch(msg.PullRequests, m.sess.Worktrees[0].Worktree.Branch)
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
		if m.sess.IsAttached {
			return m, nil
		}
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
				provider.DeleteSession(m.sess.ID, m.sess.IsCreating()),
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

func RenderPanel(s *session.Session, prs []git.PullRequest, width, height int) string {
	if s == nil {
		return ""
	}
	m := Model{sess: s, prs: prs, width: width, height: height}
	return m.View()
}

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

func (m Model) View() string {
	if m.sess == nil {
		return "No session selected"
	}

	var b strings.Builder
	s := m.sess

	name := titleStyle().Render(s.Name)
	pill := statusPill(s.Status)
	if m.width > 0 {
		gap := max(m.width-lipgloss.Width(name)-lipgloss.Width(pill), 1)
		b.WriteString(name + strings.Repeat(" ", gap) + pill)
	} else {
		b.WriteString(name + "  " + pill)
	}
	b.WriteString("\n")

	ruleWidth := m.width
	if ruleWidth < 1 {
		ruleWidth = 40
	}
	b.WriteString(ruleStyle().Render(strings.Repeat("─", ruleWidth)))
	b.WriteString("\n\n")

	b.WriteString(sectionStyle().Render("⎇ Workspace") + "\n")
	if len(s.Worktrees) > 0 {
		for i := range s.Worktrees {
			label := "workspace"
			if s.Worktrees[i].Workspace != nil {
				label = s.Worktrees[i].Workspace.Name
			}
			wt := s.Worktrees[i].Worktree
			if wt != nil && wt.Branch != nil {
				b.WriteString(labelStyle().Render(label) + valueStyle().Render(wt.Branch.Name) + "\n")
			} else {
				b.WriteString(labelStyle().Render(label) + lipgloss.NewStyle().Foreground(theme.Current.TextMuted).Render("—") + "\n")
			}
		}
	} else {
		b.WriteString(labelStyle().Render("") + valueStyle().Render(s.WorkspaceDisplay()) + "\n")
	}
	if s.SessionRoot != "" {
		pathStyle := lipgloss.NewStyle().Foreground(theme.Current.Path)
		b.WriteString(labelStyle().Render("Root") + pathStyle.Render(s.SessionRoot) + "\n")
	}
	for _, pr := range m.prs {
		stateStyle := lipgloss.NewStyle().Foreground(prStateColor(pr.State))
		prStr := fmt.Sprintf("#%d %s ", pr.Number, pr.Title) + stateStyle.Render("["+string(pr.State)+"]")
		b.WriteString(labelStyle().Render("PR") + prStr + "\n")
	}

	if s.TmuxSession != nil {
		b.WriteString("\n" + sectionStyle().Render("$ Tmux") + "\n")
		b.WriteString(labelStyle().Render("Session") + valueStyle().Render(s.TmuxSession.Name) + "\n")
		if s.TmuxSession.StartDir != "" {
			pathStyle := lipgloss.NewStyle().Foreground(theme.Current.Path)
			b.WriteString(labelStyle().Render("Dir") + pathStyle.Render(s.TmuxSession.StartDir) + "\n")
		}
	}

	if len(s.ClaudeSessions) > 0 {
		b.WriteString("\n" + sectionStyle().Render("◆ Claude") + "\n")
		for _, cs := range s.ClaudeSessions {
			b.WriteString(labelStyle().Render("Status") + valueStyle().Render(string(cs.Status)) + "\n")
		}
	}

	if s.StatusError != "" {
		b.WriteString("\n" + warningStyle().Render("[!] "+s.StatusError) + "\n")
	}

	var actionErrors []string
	for _, a := range s.SessionActions {
		if a.Error != "" {
			actionErrors = append(actionErrors, fmt.Sprintf("[!] action %s failed: %s", a.Type, a.Error))
		}
	}
	if len(actionErrors) > 0 {
		b.WriteString("\n" + sectionStyle().Render("⚠ Actions") + "\n")
		for _, e := range actionErrors {
			b.WriteString(warningStyle().Render(e) + "\n")
		}
	}

	return padToHeight(b.String(), m.height-1)
}
