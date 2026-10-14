package ui

import (
	"strings"

	"github.com/eleonorayaya/utena/internal/tui/theme"
)

type StepState int

const (
	StepPending StepState = iota
	StepRunning
	StepDone
	StepFailed
)

type Step struct {
	Label string
	State StepState
	Error string
}

func StepList(steps []Step, spinner string, width int) string {
	var lines []string
	for _, s := range steps {
		var glyph string
		labelStyle := fg(theme.Current.Text)
		switch s.State {
		case StepDone:
			glyph = fg(theme.Current.StatusReady).Render("✓")
		case StepRunning:
			glyph = fg(theme.Current.Primary).Render(spinner)
		case StepFailed:
			glyph = fg(theme.Current.Error).Render("✕")
		default:
			glyph = fg(theme.Current.TextMuted).Render("·")
			labelStyle = fg(theme.Current.TextMuted)
		}
		lines = append(lines, clampWidth("  "+glyph+" "+labelStyle.Render(Truncate(s.Label, max(width-4, 1))), width))
		if s.State == StepFailed && s.Error != "" {
			text := SingleLine(s.Error)
			lines = append(lines, clampWidth("    "+fg(theme.Current.Error).Render(Truncate(text, max(width-4, 1))), width))
		}
	}
	return strings.Join(lines, "\n")
}
