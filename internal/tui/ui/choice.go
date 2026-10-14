package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

type Choice struct {
	Options  []string
	Selected int
}

func (c Choice) Value() string {
	if c.Selected < 0 || c.Selected >= len(c.Options) {
		return ""
	}
	return c.Options[c.Selected]
}

func (c Choice) Update(msg tea.KeyMsg) (Choice, bool) {
	if len(c.Options) == 0 {
		return c, false
	}
	switch msg.String() {
	case "right", "l", " ", "space":
		c.Selected = (c.Selected + 1) % len(c.Options)
	case "left", "h":
		c.Selected = (c.Selected - 1 + len(c.Options)) % len(c.Options)
	default:
		return c, false
	}
	return c, true
}

func (c Choice) View(focused bool) string {
	parts := make([]string, len(c.Options))
	for i, o := range c.Options {
		switch {
		case i == c.Selected && focused:
			parts[i] = fg(theme.Current.Primary).Render("● " + o)
		case i == c.Selected:
			parts[i] = fg(theme.Current.TextEmphasis).Render("● " + o)
		default:
			parts[i] = fg(theme.Current.TextMuted).Render("○ " + o)
		}
	}
	return strings.Join(parts, "   ")
}
