package tui

import (
	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/tui/router"
)

type helpModel struct {
	inner   help.Model
	visible bool
}

func newHelpModel(visible bool) helpModel {
	return helpModel{
		inner:   help.New(),
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
