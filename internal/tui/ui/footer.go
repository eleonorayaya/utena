package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

type StatusKind int

const (
	StatusInfo StatusKind = iota
	StatusWarning
	StatusError
)

type Status struct {
	Text string
	Kind StatusKind
}

func (s Status) Empty() bool {
	return s.Text == ""
}

type StatusReporter interface {
	Status() Status
}

var MoreKey = key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "more"))

const footerSep = " · "

func statusStyle(k StatusKind) lipgloss.Style {
	switch k {
	case StatusWarning:
		return fg(theme.Current.StatusActive)
	case StatusError:
		return fg(theme.Current.Error)
	default:
		return fg(theme.Current.TextMuted)
	}
}

func renderBinding(b key.Binding) string {
	h := b.Help()
	return fg(theme.Current.Primary).Render(h.Key) + " " + fg(theme.Current.TextMuted).Render(h.Desc)
}

func Footer(bindings []key.Binding, status Status, width int, showMore bool) string {
	if !status.Empty() {
		text := SingleLine(status.Text)
		return statusStyle(status.Kind).Render(Truncate(text, width))
	}

	var tail string
	used := 0
	if showMore {
		tail = renderBinding(MoreKey)
		used = lipgloss.Width(tail)
	}

	var parts []string
	for _, b := range bindings {
		if !b.Enabled() || b.Help().Key == "" {
			continue
		}
		p := renderBinding(b)
		w := lipgloss.Width(p)
		if used > 0 {
			w += len(footerSep)
		}
		if used+w > width {
			break
		}
		parts = append(parts, p)
		used += w
	}
	if showMore {
		parts = append(parts, tail)
	}
	return clampWidth(strings.Join(parts, fg(theme.Current.TextMuted).Render(footerSep)), width)
}

func Clamp(s string, width int) string {
	if width <= 0 {
		return s
	}
	return clampWidth(s, width)
}

func clampWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}

type TextCapturer interface {
	CapturesText() bool
}
