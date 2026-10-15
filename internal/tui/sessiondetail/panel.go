package sessiondetail

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

const prNumW = 6

func titleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(theme.Current.TextEmphasis)
}

func valueStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.Text)
}

func mutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.TextMuted)
}

func warningStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.AccentLavender)
}

func ruleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.SurfaceVariant)
}

func workspaceStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.Tertiary).Bold(true)
}

func statusPill(status session.SessionStatus) string {
	return lipgloss.NewStyle().
		Foreground(theme.Current.TextOnPrimary).
		Background(ui.SessionStatusColor(status)).
		Bold(true).
		Render(" " + string(status) + " ")
}

func RenderPanel(s *session.Session, width, height int) string {
	m := Model{sess: s, width: width, height: height}
	return m.View()
}

func (m Model) View() string {
	width := m.width
	if width < 1 {
		width = 60
	}
	if m.sess == nil {
		return ui.FitHeight(mutedStyle().Render("Select a session"), m.height)
	}
	s := *m.sess

	var lines []string
	lines = append(lines, titleLine(s, width), statusLine(s, width), ruleStyle().Render(strings.Repeat("─", width)), "")

	if problems := problemLines(s); len(problems) > 0 {
		for _, p := range problems {
			lines = append(lines, warningStyle().Render(ui.Truncate("⚠ "+p, width)))
		}
		lines = append(lines, "")
	}

	if open := s.OpenBlockers(); len(open) > 0 {
		lines = append(lines, ui.SectionHeader("Blocked", len(open)))
		for i, bl := range open {
			gutter := "  "
			if m.interactive && i == m.blockerCursor {
				gutter = lipgloss.NewStyle().Foreground(theme.Current.Primary).Render("▌ ")
			}
			text := fmt.Sprintf("%s · %s · %s", bl.Kind, bl.Label(), common.TimeAgo(bl.CreatedAt))
			lines = append(lines, gutter+valueStyle().Render(ui.Truncate(text, max(width-2, 1))))
		}
		lines = append(lines, "")
	}

	for _, g := range groupPRs(s) {
		lines = append(lines, workspaceStyle().Render(ui.Truncate(g.name, width)))
		if len(g.prs) == 0 {
			lines = append(lines, "  "+mutedStyle().Render("no pull requests"))
			continue
		}
		for _, pr := range g.prs {
			lines = append(lines, prLine(pr, width))
		}
	}

	body := strings.Join(lines, "\n")
	if m.picker.IsOpen() && m.height > 0 {
		rows := m.picker.Height(m.height - 1)
		body = ui.FitHeight(body, max(m.height-rows, 1)) + "\n" + m.picker.View(width, rows)
	}
	return ui.FitHeight(body, m.height)
}

func titleLine(s session.Session, width int) string {
	var right []string
	if s.IsAttached {
		right = append(right, lipgloss.NewStyle().Foreground(theme.Current.Primary).Render("◆ attached"))
	}
	if s.Status != session.StatusActive && s.Status != "" {
		right = append(right, statusPill(s.Status))
	}
	rightStr := strings.Join(right, " ")
	rightW := lipgloss.Width(rightStr)
	if rightW >= width {
		rightStr, rightW = "", 0
	}
	nameW := max(width-rightW-1, 1)
	name := titleStyle().Render(ui.Truncate(s.DisplayLabel(), nameW))
	gap := max(width-lipgloss.Width(name)-rightW, 0)
	return name + strings.Repeat(" ", gap) + rightStr
}

func statusLine(s session.Session, width int) string {
	var parts []string
	if st := s.AttentionStatus(); st != "" {
		g := ui.ClaudeGlyph(st)
		parts = append(parts, g.Render()+" "+valueStyle().Render(ui.ClaudeStatusText(st)))
	}
	if ago := ui.ShortAgo(s.LastUsedAt); ago != "" {
		used := "used " + ago + " ago"
		if ago == "now" {
			used = "used just now"
		}
		parts = append(parts, mutedStyle().Render(used))
	}
	line := strings.Join(parts, mutedStyle().Render(" · "))
	if lipgloss.Width(line) > width {
		return ui.Truncate(ui.ClaudeStatusText(s.AttentionStatus()), width)
	}
	return line
}

func problemLines(s session.Session) []string {
	var out []string
	if s.StatusError != "" {
		out = append(out, s.StatusError)
	}
	for _, a := range s.SessionActions {
		if a.Error != "" {
			out = append(out, fmt.Sprintf("action %s failed: %s", a.Type, a.Error))
		}
	}
	return out
}

func prLine(pr git.PullRequest, width int) string {
	numText := fmt.Sprintf("#%d", pr.Number)
	num := ui.Hyperlink(pr.HTMLURL, lipgloss.NewStyle().Foreground(theme.Current.AccentBlue).Render(numText))
	numPad := strings.Repeat(" ", max(prNumW-len(numText), 0))
	badge := ui.PRBadge(pr.State)
	badgeW := lipgloss.Width(badge)

	fixed := 2 + max(prNumW, len(numText)) + 2 + 2 + badgeW
	if fixed+1 > width {
		return "  " + num + numPad
	}
	titleW := width - fixed
	title := ui.Truncate(pr.Title, titleW)
	title += strings.Repeat(" ", max(titleW-lipgloss.Width(title), 0))
	return "  " + num + numPad + "  " + valueStyle().Render(title) + "  " + badge
}
