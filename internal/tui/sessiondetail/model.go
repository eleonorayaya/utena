package sessiondetail

import (

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/addworkspaceform"
	"github.com/eleonorayaya/utena/internal/tui/blockerform"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/sessionprogress"
)

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
