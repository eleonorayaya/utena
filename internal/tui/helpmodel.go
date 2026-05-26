package tui

import (
	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

type helpModel struct {
	inner   help.Model
	visible bool
}

func newHelpModel(visible bool) helpModel {
	h := help.New()
	h.Styles.ShortKey = h.Styles.ShortKey.Foreground(theme.Current.Primary)
	h.Styles.ShortDesc = h.Styles.ShortDesc.Foreground(theme.Current.Text)
	h.Styles.ShortSeparator = h.Styles.ShortSeparator.Foreground(theme.Current.TextMuted)
	h.Styles.FullKey = h.Styles.FullKey.Foreground(theme.Current.Primary)
	h.Styles.FullDesc = h.Styles.FullDesc.Foreground(theme.Current.Text)
	h.Styles.FullSeparator = h.Styles.FullSeparator.Foreground(theme.Current.TextMuted)
	return helpModel{
		inner:   h,
		visible: visible,
	}
}

func (h helpModel) Update(msg tea.Msg) helpModel {
	switch msg := msg.(type) {
	case router.SetHelpVisibleMsg:
		h.visible = msg.Visible
	}
	return h
}

func (h helpModel) View(keymap help.KeyMap) string {
	if !h.visible {
		return ""
	}
	return h.inner.View(keymap)
}

func (h helpModel) HeightCost() int {
	if !h.visible {
		return 0
	}
	return 2
}
