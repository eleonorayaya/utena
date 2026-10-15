package main

import (
	"fmt"
	"io"
	"math"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/eleonorayaya/utena/internal/mux"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

type topSort string

const (
	sortCPU  topSort = "cpu"
	sortMem  topSort = "mem"
	sortName topSort = "name"
)

func topCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "top",
		Short:        "Show CPU and memory use per session",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Root().Flags().GetString("port")
			once, _ := cmd.Flags().GetBool("once")
			interval, _ := cmd.Flags().GetDuration("interval")
			if interval < 500*time.Millisecond {
				return fmt.Errorf("--interval must be at least 500ms")
			}

			if once {
				resp, err := fetchResources(port, 10*time.Second)
				if err != nil {
					return err
				}
				return writeResourcesTSV(cmd.OutOrStdout(), sortResources(resp.Sessions, sortCPU))
			}

			loadTheme()
			_, err := tea.NewProgram(newTopModel(port, interval), tea.WithAltScreen()).Run()
			return err
		},
	}
	cmd.Flags().Bool("once", false, "print one tab-separated snapshot and exit")
	cmd.Flags().Duration("interval", 2*time.Second, "refresh interval for the live view")
	return cmd
}

func fetchResources(port string, timeout time.Duration) (*session.SessionResourcesResponse, error) {
	resp, err := getJSON[session.SessionResourcesResponse](port, "/sessions/resources", timeout)
	if err != nil {
		return nil, fmt.Errorf("could not reach utena daemon on port %s (is it running?): %w", port, err)
	}
	return resp, nil
}

func sortResources(rows []session.SessionResources, by topSort) []session.SessionResources {
	out := append([]session.SessionResources(nil), rows...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch by {
		case sortMem:
			if a.RSSBytes != b.RSSBytes {
				return a.RSSBytes > b.RSSBytes
			}
		case sortCPU:
			if a.CPUPercent != b.CPUPercent {
				return a.CPUPercent > b.CPUPercent
			}
		}
		return a.Name < b.Name
	})
	return out
}

func writeResourcesTSV(w io.Writer, rows []session.SessionResources) error {
	if _, err := fmt.Fprintln(w, "name\tstatus\tcpu_pct\trss_bytes\tprocs"); err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := fmt.Fprintf(w, "%s\t%s\t%.1f\t%d\t%d\n", r.Name, r.Status, r.CPUPercent, r.RSSBytes, r.Procs); err != nil {
			return err
		}
	}
	return nil
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	value, suffix := float64(n)/unit, "KiB"
	for _, s := range []string{"MiB", "GiB", "TiB"} {
		if value < unit {
			break
		}
		value, suffix = value/unit, s
	}
	return fmt.Sprintf("%.1f%s", value, suffix)
}

type topKeyMap struct {
	SortCPU  key.Binding
	SortMem  key.Binding
	SortName key.Binding
	Procs    key.Binding
	Quit     key.Binding
}

var topKeys = topKeyMap{
	SortCPU:  key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "cpu")),
	SortMem:  key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "mem")),
	SortName: key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "name")),
	Procs:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "processes")),
	Quit:     key.NewBinding(key.WithKeys("q", "esc", "ctrl+c"), key.WithHelp("q", "quit")),
}

func (k topKeyMap) bindings() []key.Binding {
	return []key.Binding{k.SortCPU, k.SortMem, k.SortName, k.Procs, k.Quit}
}

const (
	topHistoryLen      = 24
	topProcsPerSession = 4
)

type resourcesMsg struct {
	resp *session.SessionResourcesResponse
	err  error
}

type topTickMsg struct{}

type topModel struct {
	interval  time.Duration
	fetch     func() (*session.SessionResourcesResponse, error)
	rows      []session.SessionResources
	history   map[string][]float64
	sampledAt time.Time
	err       error
	sortBy    topSort
	showProcs bool
	width     int
	height    int
}

func newTopModel(port string, interval time.Duration) topModel {
	return topModel{
		interval:  interval,
		sortBy:    sortCPU,
		showProcs: true,
		history:   make(map[string][]float64),
		fetch: func() (*session.SessionResourcesResponse, error) {
			return fetchResources(port, interval+5*time.Second)
		},
	}
}

func (m topModel) load() tea.Cmd {
	fetch := m.fetch
	return func() tea.Msg {
		resp, err := fetch()
		return resourcesMsg{resp: resp, err: err}
	}
}

func (m topModel) Init() tea.Cmd {
	return m.load()
}

func (m topModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, topKeys.Quit):
			return m, tea.Quit
		case key.Matches(msg, topKeys.SortCPU):
			m.sortBy = sortCPU
		case key.Matches(msg, topKeys.SortMem):
			m.sortBy = sortMem
		case key.Matches(msg, topKeys.SortName):
			m.sortBy = sortName
		case key.Matches(msg, topKeys.Procs):
			m.showProcs = !m.showProcs
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case resourcesMsg:
		m.err = msg.err
		if msg.err == nil {
			m.rows = msg.resp.Sessions
			m.sampledAt = msg.resp.SampledAt
			m.recordHistory()
		}
		return m, tea.Tick(m.interval, func(time.Time) tea.Msg { return topTickMsg{} })
	case topTickMsg:
		return m, m.load()
	}
	return m, nil
}

func (m *topModel) recordHistory() {
	next := make(map[string][]float64, len(m.rows))
	for _, r := range m.rows {
		h := append(m.history[r.MuxSession], r.CPUPercent)
		if len(h) > topHistoryLen {
			h = h[len(h)-topHistoryLen:]
		}
		next[r.MuxSession] = h
	}
	m.history = next
}

type topLayout struct {
	bar   int
	trend int
}

func layoutFor(width int) topLayout {
	switch {
	case width >= 104:
		return topLayout{bar: 10, trend: topHistoryLen / 2}
	case width >= 84:
		return topLayout{bar: 8}
	default:
		return topLayout{}
	}
}

func (m topModel) View() string {
	width := m.width
	if width <= 0 {
		width = 110
	}
	lay := layoutFor(width)

	lines := []string{m.titleLine(width), "", m.summaryLine(width), "", m.headerRow(width, lay)}
	rows := sortResources(m.rows, m.sortBy)
	var maxRSS int64
	for _, r := range rows {
		maxRSS = max(maxRSS, r.RSSBytes)
	}
	for _, r := range rows {
		lines = append(lines, m.sessionRow(r, maxRSS, width, lay))
		if m.showProcs {
			lines = append(lines, m.processRows(r, width, lay)...)
		}
	}
	if len(rows) == 0 {
		msg := "waiting for the first sample…"
		if !m.sampledAt.IsZero() {
			msg = "no processes running in any session"
		}
		lines = append(lines, "  "+ui.Muted().Italic(true).Render(msg))
	}

	body := strings.Join(lines, "\n")
	if m.height > 2 {
		body = ui.FitHeight(body, m.height-2)
	}
	status := ui.Status{}
	if m.err != nil {
		status = ui.Status{Text: m.err.Error(), Kind: ui.StatusError}
	}
	return body + "\n\n" + ui.Footer(topKeys.bindings(), status, width, false)
}

func (m topModel) titleLine(width int) string {
	t := theme.Current
	left := lipgloss.NewStyle().Foreground(t.Primary).Bold(true).Render("utena top") +
		ui.Muted().Render(fmt.Sprintf("  %d sessions", len(m.rows)))
	var right string
	if !m.sampledAt.IsZero() {
		right = ui.Muted().Render("sampled ") + ui.FG(t.Text).Render(m.sampledAt.Local().Format("15:04:05")) + ui.Muted().Render(" · ")
	}
	right += ui.Muted().Render("every " + m.interval.String())
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return ui.Clamp(left+strings.Repeat(" ", gap)+right, width)
}

func (m topModel) summaryLine(width int) string {
	var cpu float64
	var rss int64
	var procs int
	for _, r := range m.rows {
		cpu += r.CPUPercent
		rss += r.RSSBytes
		procs += r.Procs
	}
	cores := runtime.NumCPU()
	label := ui.Muted()
	value := ui.FG(theme.Current.TextEmphasis).Bold(true)
	line := label.Render("CPU ") + meter(cpu/float64(cores*100), 20, cpuStyle(cpu/float64(cores))) +
		" " + value.Render(fmt.Sprintf("%.1f%%", cpu)) + label.Render(fmt.Sprintf(" of %d cores", cores)) +
		label.Render("    MEM ") + value.Render(formatBytes(rss)) +
		label.Render("    ") + value.Render(fmt.Sprintf("%d", procs)) + label.Render(" procs")
	return ui.Clamp(line, width)
}

type topColumns struct {
	glyph, name, status, cpuBar, cpu, memBar, mem, procs, trend []ui.Span
}

func (lay topLayout) row(c topColumns, width int) string {
	cells := []ui.Cell{{Spans: c.glyph, Width: 2}, {Spans: c.name}, {Spans: c.status, Width: 12}}
	if lay.bar > 0 {
		cells = append(cells, ui.Cell{Spans: c.cpuBar, Width: lay.bar + 1})
	}
	cells = append(cells, ui.Cell{Spans: c.cpu, Width: 8, Align: ui.AlignRight}, ui.Cell{Width: 2})
	if lay.bar > 0 {
		cells = append(cells, ui.Cell{Spans: c.memBar, Width: lay.bar + 1})
	}
	cells = append(cells, ui.Cell{Spans: c.mem, Width: 10, Align: ui.AlignRight}, ui.Cell{Spans: c.procs, Width: 7, Align: ui.AlignRight})
	if lay.trend > 0 {
		cells = append(cells, ui.Cell{Width: 2}, ui.Cell{Spans: c.trend, Width: lay.trend})
	}
	return ui.Row(cells, width, nil)
}

func span(text string, style lipgloss.Style) []ui.Span {
	return []ui.Span{{Text: text, Style: style}}
}

type rowStyles struct {
	name, cpu, mem lipgloss.Style
}

func stylesFor(unmanaged bool, cpu float64) rowStyles {
	if unmanaged {
		return rowStyles{name: ui.Muted(), cpu: ui.Muted(), mem: ui.Muted()}
	}
	return rowStyles{name: ui.FG(theme.Current.Text), cpu: cpuStyle(cpu), mem: ui.FG(theme.Current.AccentLavender)}
}

func (m topModel) headerRow(width int, lay topLayout) string {
	t := theme.Current
	plain := lipgloss.NewStyle().Foreground(t.TextMuted).Bold(true)
	active := lipgloss.NewStyle().Foreground(t.Primary).Bold(true)
	head := func(title string, by topSort) []ui.Span {
		if by != "" && by == m.sortBy {
			return span(title+" ▼", active)
		}
		return span(title, plain)
	}
	return lay.row(topColumns{
		name:   head("SESSION", sortName),
		status: head("STATUS", ""),
		cpu:    head("CPU", sortCPU),
		mem:    head("MEM", sortMem),
		procs:  head("PROCS", ""),
		trend:  head("TREND", ""),
	}, width)
}

func (m topModel) sessionRow(r session.SessionResources, maxRSS int64, width int, lay topLayout) string {
	st := stylesFor(r.SessionID == nil, r.CPUPercent)
	glyph, glyphStyle := statusGlyph(r.Status)
	memFrac := 0.0
	if maxRSS > 0 {
		memFrac = float64(r.RSSBytes) / float64(maxRSS)
	}
	return lay.row(topColumns{
		glyph:  span(glyph, glyphStyle),
		name:   span(r.Name, st.name),
		status: span(r.Status, ui.Muted()),
		cpuBar: span(meter(r.CPUPercent/100, lay.bar, st.cpu), lipgloss.NewStyle()),
		cpu:    span(fmt.Sprintf("%.1f%%", r.CPUPercent), st.cpu),
		memBar: span(meter(memFrac, lay.bar, st.mem), lipgloss.NewStyle()),
		mem:    span(formatBytes(r.RSSBytes), st.name),
		procs:  span(fmt.Sprintf("%d", r.Procs), ui.Muted()),
		trend:  span(sparkline(m.history[r.MuxSession], lay.trend), st.cpu),
	}, width)
}

func statusGlyph(status string) (string, lipgloss.Style) {
	t := theme.Current
	switch session.SessionStatus(status) {
	case session.StatusActive:
		return "●", ui.FG(t.StatusReady)
	case session.StatusCompleted:
		return "✓", ui.FG(t.StatusDone)
	case session.StatusCreating, session.StatusPending:
		return "◐", ui.FG(t.StatusPending)
	case session.StatusBroken:
		return "✕", ui.FG(t.Error)
	case session.StatusInactive, session.StatusArchived:
		return "○", ui.Muted()
	default:
		return "◌", ui.Muted()
	}
}

func cpuStyle(pct float64) lipgloss.Style {
	t := theme.Current
	switch {
	case pct >= 100:
		return ui.FG(t.Error)
	case pct >= 50:
		return ui.FG(t.StatusActive)
	default:
		return ui.FG(t.StatusReady)
	}
}

var meterEighths = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

func meter(frac float64, width int, style lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	frac = math.Max(0, math.Min(frac, 1))
	eighths := int(math.Round(frac * float64(width*8)))
	full, part := eighths/8, eighths%8
	filled := strings.Repeat("█", full) + meterEighths[part]
	track := strings.Repeat("·", width-lipgloss.Width(filled))
	return style.Render(filled) + ui.FG(theme.Current.SurfaceVariant).Render(track)
}

var sparkLevels = []rune("▁▂▃▄▅▆▇█")

func sparkline(values []float64, width int) string {
	if width <= 0 || len(values) == 0 {
		return ""
	}
	if len(values) > width {
		values = values[len(values)-width:]
	}
	peak := 25.0
	for _, v := range values {
		peak = math.Max(peak, v)
	}
	out := make([]rune, len(values))
	for i, v := range values {
		level := int(math.Round(math.Max(v, 0) / peak * float64(len(sparkLevels)-1)))
		out[i] = sparkLevels[level]
	}
	return string(out)
}

func sortProcesses(procs []mux.ProcessUsage, by topSort) []mux.ProcessUsage {
	out := append([]mux.ProcessUsage(nil), procs...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch by {
		case sortMem:
			if a.RSSBytes != b.RSSBytes {
				return a.RSSBytes > b.RSSBytes
			}
		case sortName:
			if a.Command != b.Command {
				return a.Command < b.Command
			}
		default:
			if a.CPUPercent != b.CPUPercent {
				return a.CPUPercent > b.CPUPercent
			}
			if a.RSSBytes != b.RSSBytes {
				return a.RSSBytes > b.RSSBytes
			}
		}
		return a.PID < b.PID
	})
	return out
}

func (m topModel) processRows(r session.SessionResources, width int, lay topLayout) []string {
	procs := sortProcesses(r.Processes, m.sortBy)
	hidden := 0
	if len(procs) > topProcsPerSession {
		hidden = len(procs) - topProcsPerSession
		procs = procs[:topProcsPerSession]
	}
	tree := ui.FG(theme.Current.SurfaceVariant)

	var lines []string
	for i, p := range procs {
		branch := "├ "
		if i == len(procs)-1 && hidden == 0 {
			branch = "└ "
		}
		st := stylesFor(r.SessionID == nil, p.CPUPercent)
		lines = append(lines, lay.row(topColumns{
			name:  []ui.Span{{Text: branch, Style: tree}, {Text: p.Command, Style: st.name}},
			cpu:   span(fmt.Sprintf("%.1f%%", p.CPUPercent), st.cpu),
			mem:   span(formatBytes(p.RSSBytes), ui.Muted()),
			procs: span(fmt.Sprintf("%d", p.PID), ui.Muted()),
		}, width))
	}
	if hidden > 0 {
		lines = append(lines, lay.row(topColumns{
			name: []ui.Span{{Text: "└ ", Style: tree}, {Text: fmt.Sprintf("+%d more", hidden), Style: ui.Muted().Italic(true)}},
		}, width))
	}
	return lines
}
