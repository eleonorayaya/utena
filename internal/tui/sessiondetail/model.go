package sessiondetail

import (
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/addworkspaceform"
	"github.com/eleonorayaya/utena/internal/tui/blockerform"
	"github.com/eleonorayaya/utena/internal/tui/bundlepicker"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/sessionprogress"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

type Model struct {
	sess                   *session.Session
	pendingDeleteID        uint
	interactive            bool
	cursor                 int
	browserState           browser.SessionsView
	pendingBlockerDeleteID uint
	width                  int
	height                 int
	picker                 ui.PRPicker
	statusMsg              string
	statusKind             ui.StatusKind
	bundlePicker           bundlepicker.Model
	pickingBundles         bool
}

func New() Model {
	return Model{bundlePicker: bundlepicker.New("Plugin bundles")}
}

func (m Model) CapturesText() bool {
	return m.pickingBundles && m.bundlePicker.CapturesText()
}

func (m *Model) setStatus(text string, kind ui.StatusKind) {
	m.statusMsg = text
	m.statusKind = kind
}

func (m Model) Status() ui.Status {
	if m.pickingBundles {
		return ui.Status{}
	}
	if m.picker.IsOpen() {
		return ui.Status{Text: "↑/↓ choose · enter open · esc cancel", Kind: ui.StatusInfo}
	}
	return ui.Status{Text: m.statusMsg, Kind: m.statusKind}
}

func (m Model) Init() (Model, tea.Cmd) {
	if m.sess == nil {
		return m, nil
	}
	return m, tea.Batch(provider.RequestSessionsState(), provider.RequestBrowserState())
}

type tabRef struct {
	windowID string
	tabID    string
}

func focusable(w browser.WindowView, t browser.TabView) bool {
	return w.ID != "" && t.ID != ""
}

func (m Model) sessionBrowser() browser.SessionBrowser {
	if m.sess == nil {
		return browser.SessionBrowser{}
	}
	return m.browserState.Sessions[m.sess.ID]
}

func (m Model) selectableTabs() []tabRef {
	var out []tabRef
	for _, w := range m.sessionBrowser().Windows {
		for _, t := range w.Tabs {
			if focusable(w, t) {
				out = append(out, tabRef{windowID: w.ID, tabID: t.ID})
			}
		}
	}
	return out
}

func (m Model) minCursor() int {
	if len(m.sess.OpenBlockers()) > 0 {
		return 0
	}
	return -1
}

type selection struct {
	blockerID uint
	tab       tabRef
	index     int
	tabIndex  int
}

func (m Model) currentSelection() selection {
	sel := selection{index: m.cursor, tabIndex: m.cursor - len(m.sess.OpenBlockers())}
	if b := m.selectedBlocker(); b != nil {
		sel.blockerID = b.ID
	}
	if t := m.selectedTab(); t != nil {
		sel.tab = *t
	}
	return sel
}

func (m *Model) restoreSelection(sel selection) {
	open := m.sess.OpenBlockers()
	if sel.blockerID != 0 {
		for i, b := range open {
			if b.ID == sel.blockerID {
				m.cursor = i
				return
			}
		}
		m.cursor = min(sel.index, len(open)-1)
		m.clampCursor()
		return
	}
	if sel.tab != (tabRef{}) {
		tabs := m.selectableTabs()
		for i, t := range tabs {
			if t == sel.tab {
				m.cursor = len(open) + i
				return
			}
		}
		if len(tabs) > 0 {
			m.cursor = len(open) + min(sel.tabIndex, len(tabs)-1)
			return
		}
	}
	m.clampCursor()
}

func (m *Model) clampCursor() {
	last := len(m.sess.OpenBlockers()) + len(m.selectableTabs()) - 1
	m.cursor = max(min(m.cursor, last), m.minCursor())
}

func (m Model) selectedBlocker() *session.SessionBlocker {
	open := m.sess.OpenBlockers()
	if m.cursor < 0 || m.cursor >= len(open) {
		return nil
	}
	b := open[m.cursor]
	return &b
}

func (m Model) selectedTab() *tabRef {
	if m.sess == nil {
		return nil
	}
	i := m.cursor - len(m.sess.OpenBlockers())
	tabs := m.selectableTabs()
	if i < 0 || i >= len(tabs) {
		return nil
	}
	return &tabs[i]
}

func (m Model) Keys() help.KeyMap {
	if m.pickingBundles {
		return m.bundlePicker.Keys()
	}
	k := keys
	if m.selectedTab() != nil {
		k.Activate = openTabKey
	}
	k.showNext = len(m.selectableTabs()) > 0
	return k
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.bundlePicker.SetSize(msg.Width, msg.Height)
		return m, nil
	case SelectMsg:
		s := msg.Session
		m.sess = &s
		m.pendingDeleteID = 0
		m.interactive = true
		m.cursor = m.minCursor()
		m.pendingBlockerDeleteID = 0
		m.picker = m.picker.Close()
		m.pickingBundles = false
		m.setStatus("", ui.StatusInfo)
		if m.bundlePicker.Catalog() == nil {
			return m, tea.Batch(provider.RequestBrowserState(), provider.FetchBundles())
		}
		return m, provider.RequestBrowserState()
	case provider.BundlesLoadedMsg:
		if msg.Err == nil {
			m.bundlePicker = m.bundlePicker.SetCatalog(msg.Bundles)
		}
		return m, nil
	case provider.SessionBundlesSetMsg:
		if m.sess != nil && msg.SessionID == m.sess.ID {
			m.setStatus("bundles saved · run /reload-plugins and /mcp in open Claude panes", ui.StatusInfo)
		}
		return m, nil
	case bundlepicker.SelectedMsg:
		m.pickingBundles = false
		if m.sess != nil {
			return m, provider.SetSessionBundles(m.sess.ID, msg.Bundles)
		}
		return m, nil
	case bundlepicker.ClosedMsg:
		m.pickingBundles = false
		return m, nil
	case provider.BrowserStateUpdatedMsg:
		if m.sess == nil {
			m.browserState = msg.State
			return m, nil
		}
		sel := m.currentSelection()
		m.browserState = msg.State
		m.restoreSelection(sel)
		return m, nil
	case provider.ErrMsg:
		m.setStatus(msg.Err.Error(), ui.StatusError)
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
		sel := m.currentSelection()
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
		if !found || m.sess.Status == session.StatusDeleted {
			m.pickingBundles = false
		}
		m.restoreSelection(sel)
		return m, nil
	case tea.KeyMsg:
		if !m.pickingBundles {
			return m.onKeyMsg(msg)
		}
	}
	if m.pickingBundles {
		var cmd tea.Cmd
		m.bundlePicker, cmd = m.bundlePicker.Update(msg)
		return m, cmd
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
		m.cursor--
		m.clampCursor()
		return m, nil

	case key.Matches(msg, keys.Down):
		m.cursor++
		m.clampCursor()
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
		if t := m.selectedTab(); t != nil {
			return m, provider.FocusTab(m.sess.ID, t.windowID, t.tabID)
		}
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

	case key.Matches(msg, keys.Bundles):
		if m.sess.Status == session.StatusDeleted {
			return m, nil
		}
		m.bundlePicker = m.bundlePicker.Open(m.sess.Bundles)
		m.pickingBundles = true
		return m, nil
	}

	return m, nil
}
