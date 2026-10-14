package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

type helpModel struct {
	inner help.Model
	full  bool
}

func newHelpModel() helpModel {
	h := help.New()
	h.Styles.FullKey = h.Styles.FullKey.Foreground(theme.Current.Primary)
	h.Styles.FullDesc = h.Styles.FullDesc.Foreground(theme.Current.Text)
	h.Styles.FullSeparator = h.Styles.FullSeparator.Foreground(theme.Current.TextMuted)
	return helpModel{inner: h}
}

func (h helpModel) SetFull(full bool) helpModel {
	h.full = full
	return h
}

func (h helpModel) Footer(keymap help.KeyMap, status ui.Status, width int, showMore bool) string {
	return ui.Footer(keymap.ShortHelp(), status, width, showMore)
}

func (h helpModel) FullView(keymap help.KeyMap, width int) string {
	h.inner.Width = width
	title := lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true).Render("Keys")
	hint := lipgloss.NewStyle().Foreground(theme.Current.TextMuted).Render("? or esc to close")
	return title + "\n\n" + h.inner.FullHelpView(keymap.FullHelp()) + "\n\n" + hint
}

func (h helpModel) HeightCost() int {
	return 2
}
