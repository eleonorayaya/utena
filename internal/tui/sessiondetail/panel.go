package sessiondetail

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

const (
	prNumW   = 6
	badgeW   = 8
	padW     = 2
	minCardW = 8
)

var pad = strings.Repeat(" ", padW)

func titleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(theme.Current.TextEmphasis)
}

func valueStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.Text)
}

func mutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.TextMuted)
}

func ruleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.TextMuted)
}

func sectionTitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true)
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
	return RenderPanelWithBrowser(s, browser.SessionsView{}, width, height)
}

func RenderPanelWithBrowser(s *session.Session, state browser.SessionsView, width, height int) string {
	m := Model{sess: s, browserState: state, width: width, height: height}
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
	bw := max(width-2*padW, 1)

	body := []string{statusLine(s, bw), ""}

	if problems := problemLines(s); len(problems) > 0 {
		errStyle := lipgloss.NewStyle().Foreground(theme.Current.Error)
		var rows []string
		for _, p := range problems {
			rows = append(rows, errStyle.Render("⚠ "+ui.SingleLine(p)))
		}
		body = append(body, card("Problems", len(problems), rows, theme.Current.Error, bw)...)
		body = append(body, "")
	}

	if open := s.OpenBlockers(); len(open) > 0 {
		var rows []string
		for i, bl := range open {
			text := valueStyle().Render(fmt.Sprintf("%s · %s · %s", bl.Kind, ui.SingleLine(bl.Label()), common.TimeAgo(bl.CreatedAt)))
			if m.interactive {
				gutter := "  "
				if i == m.cursor {
					gutter = lipgloss.NewStyle().Foreground(theme.Current.Primary).Render("▌ ")
				}
				text = gutter + text
			}
			rows = append(rows, text)
		}
		body = append(body, card("Blocked", len(open), rows, theme.Current.StatusActive, bw)...)
		body = append(body, "")
	}

	if rows := workspaceRows(s, bw); len(rows) > 0 {
		body = append(body, sectionRule(sectionTitleStyle().Render("Workspaces"), mutedStyle().Render(fmt.Sprint(len(rows))), bw))
		body = append(body, rows...)
		body = append(body, "")
	}

	if rows := m.browserRows(bw); len(rows) > 0 {
		body = append(body, rows...)
		body = append(body, "")
	}

	for _, g := range groupPRs(s) {
		body = append(body, sectionRule(workspaceStyle().Render(ui.SingleLine(g.name)), mutedStyle().Render(prSummary(g.prs)), bw))
		for _, pr := range g.prs {
			body = append(body, prLine(pr, bw))
		}
		body = append(body, "")
	}

	lines := []string{headerBand(s, width)}
	for _, l := range body {
		if l == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, ui.Clamp(pad+l, width))
	}

	out := strings.Join(lines, "\n")
	if m.picker.IsOpen() && m.height > 0 {
		rows := m.picker.Height(m.height - 1)
		out = ui.FitHeight(out, max(m.height-rows, 1)) + "\n" + m.picker.View(width, rows)
	}
	return ui.FitHeight(out, m.height)
}

func headerBand(s session.Session, width int) string {
	bg := lipgloss.NewStyle().Background(theme.Current.Selection)
	inner := max(width-2*padW, 1)

	var right []string
	if s.IsAttached {
		right = append(right, bg.Foreground(theme.Current.Primary).Render("◆ attached"))
	}
	if s.Status != session.StatusActive && s.Status != "" {
		right = append(right, statusPill(s.Status))
	}
	rightStr := strings.Join(right, bg.Render(" "))
	rightW := lipgloss.Width(rightStr)
	if rightW >= inner {
		rightStr, rightW = "", 0
	}

	nameW := max(inner-rightW-1, 1)
	name := titleStyle().Background(theme.Current.Selection).Render(ui.Truncate(ui.SingleLine(s.DisplayLabel()), nameW))
	gap := max(inner-lipgloss.Width(name)-rightW, 0)
	line := bg.Render(pad) + name + bg.Render(strings.Repeat(" ", gap)) + rightStr + bg.Render(pad)
	return ui.Clamp(line, width)
}

func statusLine(s session.Session, width int) string {
	var parts []string
	if st := s.AttentionStatus(); st != "" {
		g := ui.ClaudeGlyph(st)
		parts = append(parts, g.Render()+" "+valueStyle().Render(ui.ClaudeStatusText(st)))
	}
	if used := agoText("used", ui.ShortAgo(s.LastUsedAt)); used != "" {
		parts = append(parts, mutedStyle().Render(used))
	}
	if created := agoText("created", ui.ShortAgo(s.CreatedAt)); created != "" {
		parts = append(parts, mutedStyle().Render(created))
	}
	return ui.Truncate(strings.Join(parts, mutedStyle().Render(" · ")), width)
}

func agoText(verb, ago string) string {
	switch ago {
	case "":
		return ""
	case "now":
		return verb + " just now"
	default:
		return verb + " " + ago + " ago"
	}
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

func sectionRule(title, info string, width int) string {
	infoW := lipgloss.Width(info)
	if infoW > 0 && lipgloss.Width(title)+infoW+4 > width {
		info, infoW = "", 0
	}
	title = ui.Truncate(title, max(width-2, 1))
	used := lipgloss.Width(title) + 1
	if infoW > 0 {
		used += infoW + 1
	}
	ruleW := width - used
	if ruleW < 1 {
		return title
	}
	line := title + " " + ruleStyle().Render(strings.Repeat("─", ruleW))
	if infoW > 0 {
		line += " " + info
	}
	return line
}

func card(title string, count int, rows []string, color lipgloss.Color, width int) []string {
	if width < minCardW {
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = ui.Truncate(r, width)
		}
		return out
	}
	border := lipgloss.NewStyle().Foreground(color)
	label := lipgloss.NewStyle().Foreground(color).Bold(true).Render(title) + mutedStyle().Render(fmt.Sprintf(" %d", count))
	label = ui.Truncate(label, width-6)
	fill := max(width-5-lipgloss.Width(label), 0)
	out := []string{border.Render("╭─ ") + label + border.Render(" "+strings.Repeat("─", fill)+"╮")}

	inner := width - 4
	for _, r := range rows {
		r = ui.Truncate(r, inner)
		out = append(out, border.Render("│ ")+r+strings.Repeat(" ", max(inner-lipgloss.Width(r), 0))+border.Render(" │"))
	}
	out = append(out, border.Render("╰"+strings.Repeat("─", width-2)+"╯"))
	return out
}

func workspaceRows(s session.Session, width int) []string {
	if len(s.Worktrees) == 0 {
		return nil
	}
	wts := append([]session.SessionWorktree(nil), s.Worktrees...)
	sort.SliceStable(wts, func(i, j int) bool { return wts[i].Position < wts[j].Position })

	type row struct{ name, branch string }
	rows := make([]row, len(wts))
	nameW := 0
	for i, sw := range wts {
		r := row{name: "workspace"}
		if sw.Workspace != nil && sw.Workspace.Name != "" {
			r.name = ui.SingleLine(sw.Workspace.Name)
		}
		if sw.Worktree != nil && sw.Worktree.Branch != nil {
			r.branch = ui.SingleLine(sw.Worktree.Branch.Name)
		}
		rows[i] = r
		nameW = max(nameW, lipgloss.Width(r.name))
	}
	inner := max(width-2, 1)
	nameW = min(nameW, max(inner/2, 1))

	out := make([]string, len(rows))
	for i, r := range rows {
		name := ui.Truncate(r.name, nameW)
		line := "  " + valueStyle().Render(name)
		if branchW := inner - nameW - 2; r.branch != "" && branchW > 0 {
			line += strings.Repeat(" ", nameW-lipgloss.Width(name)+2) + mutedStyle().Render(ui.Truncate(r.branch, branchW))
		}
		out[i] = line
	}
	return out
}

func prSummary(prs []git.PullRequest) string {
	if len(prs) == 0 {
		return "no pull requests"
	}
	counts := map[git.PRState]int{}
	for _, pr := range prs {
		counts[pr.State]++
	}
	var parts []string
	for _, st := range []git.PRState{git.PRStateOpen, git.PRStateDraft, git.PRStateMerged, git.PRStateClosed} {
		if n := counts[st]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, st))
		}
	}
	return strings.Join(parts, " · ")
}

func checksGlyph(state git.ChecksState) string {
	switch state {
	case git.ChecksStatePassed:
		return lipgloss.NewStyle().Foreground(theme.Current.StatusReady).Render("✓")
	case git.ChecksStateFailed, git.ChecksStateFailing:
		return lipgloss.NewStyle().Foreground(theme.Current.Error).Render("✕")
	case git.ChecksStatePending:
		return lipgloss.NewStyle().Foreground(theme.Current.StatusActive).Render("◌")
	default:
		return " "
	}
}

func prLine(pr git.PullRequest, width int) string {
	numText := fmt.Sprintf("#%d", pr.Number)
	num := ui.Hyperlink(pr.HTMLURL, lipgloss.NewStyle().Foreground(theme.Current.AccentBlue).Render(numText))
	numPad := strings.Repeat(" ", max(prNumW-len(numText), 0))
	badge := ui.PRBadge(pr.State)
	badge += strings.Repeat(" ", max(badgeW-lipgloss.Width(badge), 0))

	fixed := 2 + max(prNumW, len(numText)) + 2 + 2 + 1 + 2 + badgeW
	if fixed+1 > width {
		return "  " + num + numPad
	}
	titleStyle := valueStyle()
	if pr.State == git.PRStateMerged || pr.State == git.PRStateClosed {
		titleStyle = mutedStyle()
	}
	titleW := width - fixed
	title := ui.Truncate(ui.SingleLine(pr.Title), titleW)
	title += strings.Repeat(" ", max(titleW-lipgloss.Width(title), 0))
	return "  " + num + numPad + "  " + titleStyle.Render(title) + "  " + checksGlyph(pr.ChecksState) + "  " + badge
}

func tabCountText(n int) string {
	if n == 1 {
		return "1 tab"
	}
	return fmt.Sprintf("%d tabs", n)
}

func tabHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

func (m Model) browserRows(width int) []string {
	sb := m.sessionBrowser()
	if len(sb.Windows) == 0 {
		return nil
	}
	rows := []string{sectionRule(sectionTitleStyle().Render("Browser"), mutedStyle().Render(tabCountText(sb.TabCount)+" · "+sb.State), width)}
	selectable := len(m.sess.OpenBlockers())
	for i, w := range sb.Windows {
		if len(sb.Windows) > 1 {
			heading := fmt.Sprintf("window %d", i+1)
			if w.State != string(browser.WindowOpen) {
				heading += " · " + w.State
			}
			rows = append(rows, "  "+mutedStyle().Render(heading))
		}
		for _, t := range w.Tabs {
			selected := false
			if focusable(w, t) {
				selected = m.interactive && m.cursor == selectable
				selectable++
			}
			rows = append(rows, tabLine(w, t, selected, width))
		}
	}
	return rows
}

func tabLine(w browser.WindowView, t browser.TabView, selected bool, width int) string {
	live := focusable(w, t)
	gutter := "  "
	if selected {
		gutter = lipgloss.NewStyle().Foreground(theme.Current.Primary).Render("▌ ")
	}
	titleStyle := valueStyle()
	markerStyle := lipgloss.NewStyle().Foreground(theme.Current.AccentBlue)
	if !live {
		titleStyle = mutedStyle()
		markerStyle = mutedStyle()
	}
	marker := " "
	switch {
	case t.State == browser.TabPending:
		marker = mutedStyle().Render("◌")
	case t.Active:
		marker = markerStyle.Render("●")
	}
	title := ui.SingleLine(t.Title)
	host := tabHost(t.URL)
	if title == "" {
		title, host = ui.SingleLine(t.URL), ""
	}
	avail := max(width-4, 1)
	hostW := min(lipgloss.Width(host), avail/3)
	titleW := avail
	if hostW > 0 {
		titleW = max(avail-hostW-2, 1)
	}
	line := gutter + marker + " " + titleStyle.Render(ui.Truncate(title, titleW))
	if hostW > 0 {
		line += "  " + mutedStyle().Render(ui.Truncate(host, hostW))
	}
	return line
}
