package sessiondetail

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
)

type SelectMsg struct {
	Session session.Session
}

func Select(s session.Session) tea.Cmd {
	return func() tea.Msg { return SelectMsg{Session: s} }
}
