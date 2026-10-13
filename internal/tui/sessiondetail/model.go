package sessiondetail

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/addworkspaceform"
	"github.com/eleonorayaya/utena/internal/tui/blockerform"
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
	sess                   *session.Session
	pendingDeleteID        uint
	interactive            bool
	blockerCursor          int
	pendingBlockerDeleteID uint
	width                  int
	height                 int
}

func New() Model {
	return Model{}
}

func (m Model) Init() (Model, tea.Cmd) {
	if m.sess == nil {
		return m, nil
	}
	return m, provider.RequestSessionsState()
}

func (m *Model) clampBlockerCursor() {
	n := len(m.sess.OpenBlockers())
	if m.blockerCursor >= n {
		m.blockerCursor = n - 1
	}
	if m.blockerCursor < 0 {
		m.blockerCursor = 0
	}
}

func (m Model) selectedBlocker() *session.SessionBlocker {
	open := m.sess.OpenBlockers()
	if m.blockerCursor < 0 || m.blockerCursor >= len(open) {
		return nil
	}
	b := open[m.blockerCursor]
	return &b
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
		m.pendingDeleteID = 0
		m.interactive = true
		m.blockerCursor = 0
		m.pendingBlockerDeleteID = 0
		return m, nil
	case provider.SessionsStateUpdatedMsg:
		if m.sess == nil {
			return m, nil
		}
		for _, s := range msg.Sessions {
			if s.ID == m.sess.ID {
				fresh := s
				m.sess = &fresh
				break
			}
		}
		m.clampBlockerCursor()
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
	pendingBlockerID := m.pendingBlockerDeleteID
	m.pendingBlockerDeleteID = 0

	switch {
	case key.Matches(msg, keys.Back):
		return m, router.Back()

	case key.Matches(msg, keys.Up):
		if m.blockerCursor > 0 {
			m.blockerCursor--
		}
		return m, nil

	case key.Matches(msg, keys.Down):
		if m.blockerCursor < len(m.sess.OpenBlockers())-1 {
			m.blockerCursor++
		}
		return m, nil

	case key.Matches(msg, keys.Block):
		if m.sess.Status == session.StatusDeleted {
			return m, nil
		}
		return m, tea.Sequence(
			router.NavigateTo(router.BlockerFormView),
			blockerform.Start(m.sess.ID),
		)

	case key.Matches(msg, keys.Resolve):
		b := m.selectedBlocker()
		if b == nil {
			return m, nil
		}
		return m, provider.ResolveBlocker(m.sess.ID, b.ID)

	case key.Matches(msg, keys.RemoveBlocker):
		b := m.selectedBlocker()
		if b == nil {
			return m, nil
		}
		if pendingBlockerID == b.ID {
			return m, provider.DeleteBlocker(m.sess.ID, b.ID)
		}
		m.pendingBlockerDeleteID = b.ID
		return m, nil

	case key.Matches(msg, keys.Activate):
		if m.sess.IsAttached {
			return m, nil
		}
		return m, provider.ActivateSession(m.sess.ID)

	case key.Matches(msg, keys.Archive):
		if !m.sess.CanArchive() {
			return m, nil
		}
		return m, tea.Batch(
			provider.ArchiveSession(m.sess.ID),
			router.Back(),
		)

	case key.Matches(msg, keys.Delete):
		// Only archived sessions can be deleted; creating sessions keep the
		// force escape hatch.
		if !m.sess.CanDelete() && !m.sess.IsCreating() {
			return m, nil
		}
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

	case key.Matches(msg, keys.AddWorkspace):
		s := m.sess.Status
		if s == session.StatusDeleted || s == session.StatusArchived || s == session.StatusCreating {
			return m, nil
		}
		id := m.sess.ID
		return m, tea.Sequence(
			router.NavigateTo(router.AddWorkspaceFormView),
			addworkspaceform.Start(id),
		)
	}

	return m, nil
}

func RenderPanel(s *session.Session, width, height int) string {
	if s == nil {
		return ""
	}
	m := Model{sess: s, width: width, height: height}
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
		mutedStyle := lipgloss.NewStyle().Foreground(theme.Current.TextMuted)
		for _, past := range s.PastBranches() {
			b.WriteString(labelStyle().Render("Past") + mutedStyle.Render(past.Name) + "\n")
		}
	} else {
		b.WriteString(labelStyle().Render("") + valueStyle().Render(s.WorkspaceDisplay()) + "\n")
	}
	if s.SessionRoot != "" {
		pathStyle := lipgloss.NewStyle().Foreground(theme.Current.Path)
		b.WriteString(labelStyle().Render("Root") + pathStyle.Render(s.SessionRoot) + "\n")
	}
	for _, pr := range s.PullRequests {
		stateStyle := lipgloss.NewStyle().Foreground(prStateColor(pr.State))
		prStr := fmt.Sprintf("#%d %s ", pr.Number, pr.Title) + stateStyle.Render("["+string(pr.State)+"]")
		b.WriteString(labelStyle().Render("PR") + prStr + "\n")
	}

	if open := s.OpenBlockers(); len(open) > 0 {
		b.WriteString("\n" + sectionStyle().Render("⏸ Blockers") + "\n")
		for i, bl := range open {
			prefix := "  "
			if m.interactive && i == m.blockerCursor {
				prefix = sectionStyle().Render("› ")
			}
			line := fmt.Sprintf("%s · %s · %s", bl.Kind, bl.Label(), common.TimeAgo(bl.CreatedAt))
			if m.pendingBlockerDeleteID == bl.ID {
				line += warningStyle().Render("  press x again to delete")
			}
			b.WriteString(prefix + valueStyle().Render(line) + "\n")
		}
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
