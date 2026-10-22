package ui

import (
	"slices"
	"strings"

	"github.com/eleonorayaya/utena/internal/session"
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

func SetupSteps(steps []session.SessionSetupStep) []Step {
	src := slices.Clone(steps)
	slices.SortStableFunc(src, func(a, b session.SessionSetupStep) int { return a.Position - b.Position })
	out := make([]Step, len(src))
	for i, s := range src {
		out[i] = Step{Label: s.Label, State: setupStepState(s.Status), Error: s.ErrorMsg}
	}
	return out
}

func setupStepState(s session.SetupStepStatus) StepState {
	switch s {
	case session.SetupStepRunning:
		return StepRunning
	case session.SetupStepDone:
		return StepDone
	case session.SetupStepFailed:
		return StepFailed
	default:
		return StepPending
	}
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
