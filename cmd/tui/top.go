package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
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
	url := fmt.Sprintf("http://localhost:%s/sessions/resources", port)
	client := &http.Client{Timeout: timeout}
	res, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("could not reach utena daemon at %s (is it running?): %w", url, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, fmt.Errorf("fetch resources: status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var resp session.SessionResourcesResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
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
	cores     int
	width     int
	height    int
}

func newTopModel(port string, interval time.Duration) topModel {
	return topModel{
		interval:  interval,
		sortBy:    sortCPU,
		showProcs: true,
		history:   make(map[string][]float64),
		cores:     runtime.NumCPU(),
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
	right := ui.Muted().Render("every " + m.interval.String())
	if !m.sampledAt.IsZero() {
		right = ui.Muted().Render("sampled ") + ui.FG(t.Text).Render(m.sampledAt.Local().Format("15:04:05")) + ui.Muted().Render(" · every "+m.interval.String())
	}
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
	cores := max(m.cores, 1)
	label := ui.Muted()
	value := ui.FG(theme.Current.TextEmphasis).Bold(true)
	line := label.Render("CPU ") + meter(cpu/float64(cores*100), 20, cpuStyle(cpu/float64(cores))) +
		" " + value.Render(fmt.Sprintf("%.1f%%", cpu)) + label.Render(fmt.Sprintf(" of %d cores", cores)) +
		label.Render("    MEM ") + value.Render(formatBytes(rss)) +
		label.Render("    ") + value.Render(fmt.Sprintf("%d", procs)) + label.Render(" procs")
	return ui.Clamp(line, width)
}

func (m topModel) headerRow(width int, lay topLayout) string {
	t := theme.Current
	plain := lipgloss.NewStyle().Foreground(t.TextMuted).Bold(true)
	active := lipgloss.NewStyle().Foreground(t.Primary).Bold(true)
	head := func(title string, by topSort) ui.Span {
		if by != "" && by == m.sortBy {
			return ui.Span{Text: title + " ▼", Style: active}
		}
		return ui.Span{Text: title, Style: plain}
	}
	cells := []ui.Cell{
		{Width: 2},
		{Spans: []ui.Span{head("SESSION", sortName)}},
		{Spans: []ui.Span{head("STATUS", "")}, Width: 12},
	}
	if lay.bar > 0 {
		cells = append(cells, ui.Cell{Width: lay.bar + 1})
	}
	cells = append(cells, ui.Cell{Spans: []ui.Span{head("CPU", sortCPU)}, Width: 8, Align: ui.AlignRight}, ui.Cell{Width: 2})
	if lay.bar > 0 {
		cells = append(cells, ui.Cell{Width: lay.bar + 1})
	}
	cells = append(cells, ui.Cell{Spans: []ui.Span{head("MEM", sortMem)}, Width: 10, Align: ui.AlignRight},
		ui.Cell{Spans: []ui.Span{head("PROCS", "")}, Width: 7, Align: ui.AlignRight})
	if lay.trend > 0 {
		cells = append(cells, ui.Cell{Width: 2}, ui.Cell{Spans: []ui.Span{head("TREND", "")}, Width: lay.trend})
	}
	return ui.Row(cells, width, nil)
}

func (m topModel) sessionRow(r session.SessionResources, maxRSS int64, width int, lay topLayout) string {
	t := theme.Current
	unmanaged := r.SessionID == nil
	glyph, glyphStyle := statusGlyph(r.Status)
	nameStyle := ui.FG(t.Text)
	if unmanaged {
		nameStyle = ui.Muted()
	}
	cs := cpuStyle(r.CPUPercent)
	ms := ui.FG(t.AccentLavender)
	if unmanaged {
		cs, ms = ui.Muted(), ui.Muted()
	}

	cells := []ui.Cell{
		{Spans: []ui.Span{{Text: glyph, Style: glyphStyle}}, Width: 2},
		{Spans: []ui.Span{{Text: r.Name, Style: nameStyle}}},
		{Spans: []ui.Span{{Text: r.Status, Style: ui.Muted()}}, Width: 12},
	}
	if lay.bar > 0 {
		cells = append(cells, ui.Cell{Spans: []ui.Span{{Text: meter(r.CPUPercent/100, lay.bar, cs)}}, Width: lay.bar + 1})
	}
	cells = append(cells,
		ui.Cell{Spans: []ui.Span{{Text: fmt.Sprintf("%.1f%%", r.CPUPercent), Style: cs}}, Width: 8, Align: ui.AlignRight},
		ui.Cell{Width: 2})
	if lay.bar > 0 {
		frac := 0.0
		if maxRSS > 0 {
			frac = float64(r.RSSBytes) / float64(maxRSS)
		}
		cells = append(cells, ui.Cell{Spans: []ui.Span{{Text: meter(frac, lay.bar, ms)}}, Width: lay.bar + 1})
	}
	cells = append(cells,
		ui.Cell{Spans: []ui.Span{{Text: formatBytes(r.RSSBytes), Style: nameStyle}}, Width: 10, Align: ui.AlignRight},
		ui.Cell{Spans: []ui.Span{{Text: fmt.Sprintf("%d", r.Procs), Style: ui.Muted()}}, Width: 7, Align: ui.AlignRight})
	if lay.trend > 0 {
		cells = append(cells, ui.Cell{Width: 2},
			ui.Cell{Spans: []ui.Span{{Text: sparkline(m.history[r.MuxSession], lay.trend), Style: cs}}, Width: lay.trend})
	}
	return ui.Row(cells, width, nil)
}

func statusGlyph(status string) (string, lipgloss.Style) {
	t := theme.Current
	switch session.SessionStatus(status) {
	case session.StatusActive:
		return "●", ui.FG(t.StatusReady)
	case session.StatusCompleted:
		return "✓", ui.FG(t.StatusDone)
	case session.StatusCreating, session.StatusPending:
		return "◐", ui.FG(t.StatusActive)
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
	t := theme.Current
	tree := ui.FG(t.SurfaceVariant)
	unmanaged := r.SessionID == nil

	var lines []string
	for i, p := range procs {
		branch := "├ "
		if i == len(procs)-1 && hidden == 0 {
			branch = "└ "
		}
		nameStyle := ui.FG(t.Text)
		cs := cpuStyle(p.CPUPercent)
		if unmanaged {
			nameStyle, cs = ui.Muted(), ui.Muted()
		}
		command := p.Command
		if command == "" {
			command = "?"
		}
		cells := []ui.Cell{
			{Width: 2},
			{Spans: []ui.Span{{Text: branch, Style: tree}, {Text: command, Style: nameStyle}}},
			{Width: 12},
		}
		if lay.bar > 0 {
			cells = append(cells, ui.Cell{Width: lay.bar + 1})
		}
		cells = append(cells,
			ui.Cell{Spans: []ui.Span{{Text: fmt.Sprintf("%.1f%%", p.CPUPercent), Style: cs}}, Width: 8, Align: ui.AlignRight},
			ui.Cell{Width: 2})
		if lay.bar > 0 {
			cells = append(cells, ui.Cell{Width: lay.bar + 1})
		}
		cells = append(cells,
			ui.Cell{Spans: []ui.Span{{Text: formatBytes(p.RSSBytes), Style: ui.Muted()}}, Width: 10, Align: ui.AlignRight},
			ui.Cell{Spans: []ui.Span{{Text: fmt.Sprintf("%d", p.PID), Style: ui.Muted()}}, Width: 7, Align: ui.AlignRight})
		if lay.trend > 0 {
			cells = append(cells, ui.Cell{Width: 2 + lay.trend})
		}
		lines = append(lines, ui.Row(cells, width, nil))
	}
	if hidden > 0 {
		more := ui.Row([]ui.Cell{
			{Width: 2},
			{Spans: []ui.Span{{Text: "└ ", Style: tree}, {Text: fmt.Sprintf("+%d more", hidden), Style: ui.Muted().Italic(true)}}},
		}, width, nil)
		lines = append(lines, more)
	}
	return lines
}
