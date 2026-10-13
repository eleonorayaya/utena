package tui

import (
	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

type helpModel struct {
	inner   help.Model
	visible bool
	full    bool
}

func newHelpModel(visible bool) helpModel {
	h := help.New()
	h.Styles.FullKey = h.Styles.FullKey.Foreground(theme.Current.Primary)
	h.Styles.FullDesc = h.Styles.FullDesc.Foreground(theme.Current.Text)
	h.Styles.FullSeparator = h.Styles.FullSeparator.Foreground(theme.Current.TextMuted)
	return helpModel{inner: h, visible: visible}
}

func (h helpModel) Update(msg tea.Msg) helpModel {
	switch msg := msg.(type) {
	case router.SetHelpVisibleMsg:
		h.visible = msg.Visible
		if !msg.Visible {
			h.full = false
		}
	}
	return h
}

func (h helpModel) SetFull(full bool) helpModel {
	h.full = full
	return h
}

func (h helpModel) Footer(keymap help.KeyMap, status ui.Status, width int, showMore bool) string {
	if !h.visible {
		return ""
	}
	return ui.Footer(keymap.ShortHelp(), status, width, showMore)
}

func (h helpModel) FullView(keymap help.KeyMap, width int) string {
	h.inner.Width = width
	title := lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true).Render("Keys")
	hint := lipgloss.NewStyle().Foreground(theme.Current.TextMuted).Render("? or esc to close")
	return title + "\n\n" + h.inner.FullHelpView(keymap.FullHelp()) + "\n\n" + hint
}

func (h helpModel) HeightCost() int {
	if !h.visible {
		return 0
	}
	return 2
}
