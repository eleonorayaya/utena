package sessiondetail

import (
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/addworkspaceform"
	"github.com/eleonorayaya/utena/internal/tui/blockerform"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/sessionprogress"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

type Model struct {
	sess                   *session.Session
	pendingDeleteID        uint
	interactive            bool
	blockerCursor          int
	pendingBlockerDeleteID uint
	width                  int
	height                 int
	picker                 ui.PRPicker
	statusMsg              string
	statusKind             ui.StatusKind
}

func New() Model {
	return Model{}
}

func (m *Model) setStatus(text string, kind ui.StatusKind) {
	m.statusMsg = text
	m.statusKind = kind
}

func (m Model) Status() ui.Status {
	if m.picker.IsOpen() {
		return ui.Status{Text: "↑/↓ choose · enter open · esc cancel", Kind: ui.StatusInfo}
	}
	return ui.Status{Text: m.statusMsg, Kind: m.statusKind}
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
		m.picker = m.picker.Close()
		m.setStatus("", ui.StatusInfo)
		return m, nil
	case ui.OpenURLFailedMsg:
		m.setStatus("could not open PR: "+msg.Err.Error(), ui.StatusError)
		return m, nil
	case provider.SessionSwitchedMsg:
		return m, tea.Quit
	case provider.SessionsStateUpdatedMsg:
		if m.sess == nil {
			return m, nil
		}
		found := false
		for _, s := range msg.Sessions {
			if s.ID == m.sess.ID {
				fresh := s
				m.sess = &fresh
				found = true
				break
			}
		}
		if found {
			m.picker = m.picker.Sync(m.sess.PullRequests)
		} else {
			m.picker = m.picker.Close()
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

	if m.picker.IsOpen() {
		var url string
		m.picker, url = m.picker.Update(msg)
		if url != "" {
			return m, provider.OpenURL(m.sess.ID, url)
		}
		return m, nil
	}
	m.setStatus("", ui.StatusInfo)

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
		m.setStatus("press x again to delete this blocker", ui.StatusWarning)
		return m, nil

	case key.Matches(msg, keys.Activate):
		if m.sess.IsAttached {
			return m, nil
		}
		return m, provider.ActivateSession(m.sess.ID)

	case key.Matches(msg, keys.OpenPR):
		prs := ui.OpenablePRs(m.sess.PullRequests)
		switch len(prs) {
		case 0:
			m.setStatus("no open pull requests", ui.StatusInfo)
			return m, nil
		case 1:
			return m, provider.OpenURL(m.sess.ID, prs[0].HTMLURL)
		default:
			m.picker = ui.NewPRPicker(prs)
			return m, nil
		}

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
		m.setStatus(fmt.Sprintf("press d again to delete %s (removes worktrees)", m.sess.DisplayLabel()), ui.StatusWarning)
		return m, nil

	case key.Matches(msg, keys.Repair):
		if m.sess.Status != session.StatusBroken {
			return m, nil
		}
		id := m.sess.ID
		return m, tea.Sequence(
			router.NavigateTo(router.SessionProgressView),
			sessionprogress.StartRepair(id),
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
