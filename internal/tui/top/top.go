package top

import (
	"fmt"
	"io"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eleonorayaya/utena/internal/mux"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

type Sort string

const (
	SortCPU  Sort = "cpu"
	SortMem  Sort = "mem"
	SortName Sort = "name"
	SortDisk Sort = "disk"
)

func SortResources(rows []session.SessionResources, by Sort) []session.SessionResources {
	out := append([]session.SessionResources(nil), rows...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch by {
		case SortMem:
			if a.RSSBytes != b.RSSBytes {
				return a.RSSBytes > b.RSSBytes
			}
		case SortCPU:
			if a.CPUPercent != b.CPUPercent {
				return a.CPUPercent > b.CPUPercent
			}
		case SortDisk:
			if da, db := diskBytes(a), diskBytes(b); da != db {
				return da > db
			}
		}
		return a.Name < b.Name
	})
	return out
}

func diskBytes(r session.SessionResources) int64 {
	if r.DiskBytes == nil {
		return -1
	}
	return *r.DiskBytes
}

func formatOptional(n *int64, format func(int64) string, missing string) string {
	if n == nil {
		return missing
	}
	return format(*n)
}

func formatInt(n int64) string {
	return strconv.FormatInt(n, 10)
}

func WriteTSV(w io.Writer, rows []session.SessionResources) error {
	if _, err := fmt.Fprintln(w, "name\tstatus\tcpu_pct\trss_bytes\tprocs\tdisk_bytes"); err != nil {
		return err
	}
	for _, r := range rows {
		disk := formatOptional(r.DiskBytes, formatInt, "-")
		if _, err := fmt.Fprintf(w, "%s\t%s\t%.1f\t%d\t%d\t%s\n", r.Name, r.Status, r.CPUPercent, r.RSSBytes, r.Procs, disk); err != nil {
			return err
		}
	}
	return nil
}

type keyMap struct {
	SortCPU  key.Binding
	SortMem  key.Binding
	SortName key.Binding
	SortDisk key.Binding
	Procs    key.Binding
	Quit     key.Binding
}

var keys = keyMap{
	SortCPU:  key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "cpu")),
	SortMem:  key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "mem")),
	SortName: key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "name")),
	SortDisk: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "disk")),
	Procs:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "processes")),
	Quit:     key.NewBinding(key.WithKeys("q", "esc", "ctrl+c"), key.WithHelp("q", "quit")),
}

func (k keyMap) bindings() []key.Binding {
	return []key.Binding{k.SortCPU, k.SortMem, k.SortName, k.SortDisk, k.Procs, k.Quit}
}

const (
	historyLen      = 24
	procsPerSession = 4
)

type resourcesMsg struct {
	resp *session.SessionResourcesResponse
	err  error
}

type tickMsg struct{}

type Model struct {
	interval  time.Duration
	fetch     func() (*session.SessionResourcesResponse, error)
	rows      []session.SessionResources
	history   map[string][]float64
	sampledAt time.Time
	err       error
	sortBy    Sort
	showProcs bool
	width     int
	height    int
}

func New(interval time.Duration, fetch func() (*session.SessionResourcesResponse, error)) Model {
	return Model{
		interval: interval,
		sortBy:   SortCPU,
		history:  make(map[string][]float64),
		fetch:    fetch,
	}
}

func (m Model) load() tea.Cmd {
	fetch := m.fetch
	return func() tea.Msg {
		resp, err := fetch()
		return resourcesMsg{resp: resp, err: err}
	}
}

func (m Model) Init() tea.Cmd {
	return m.load()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, keys.SortCPU):
			m.sortBy = SortCPU
		case key.Matches(msg, keys.SortMem):
			m.sortBy = SortMem
		case key.Matches(msg, keys.SortName):
			m.sortBy = SortName
		case key.Matches(msg, keys.SortDisk):
			m.sortBy = SortDisk
		case key.Matches(msg, keys.Procs):
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
		return m, tea.Tick(m.interval, func(time.Time) tea.Msg { return tickMsg{} })
	case tickMsg:
		return m, m.load()
	}
	return m, nil
}

func (m *Model) recordHistory() {
	next := make(map[string][]float64, len(m.rows))
	for _, r := range m.rows {
		h := append(m.history[r.MuxSession], r.CPUPercent)
		if len(h) > historyLen {
			h = h[len(h)-historyLen:]
		}
		next[r.MuxSession] = h
	}
	m.history = next
}

type layout struct {
	meter int
	bar   int
	trend int
}

func layoutFor(width int) layout {
	switch {
	case width >= 114:
		return layout{meter: 20, bar: 10, trend: historyLen / 2}
	case width >= 94:
		return layout{meter: 20, bar: 8}
	default:
		return layout{meter: 12}
	}
}

func (m Model) View() string {
	width := m.width
	if width <= 0 {
		width = 110
	}
	lay := layoutFor(width)

	lines := []string{m.titleLine(width), "", m.summaryLine(width, lay), "", m.headerRow(width, lay)}
	rows := SortResources(m.rows, m.sortBy)
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
	return body + "\n\n" + ui.Footer(keys.bindings(), status, width, false)
}

func (m Model) titleLine(width int) string {
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

func (m Model) summaryLine(width int, lay layout) string {
	var cpu float64
	var rss int64
	var disk *int64
	var procs int
	for _, r := range m.rows {
		cpu += r.CPUPercent
		rss += r.RSSBytes
		procs += r.Procs
		if r.DiskBytes != nil {
			if disk == nil {
				disk = new(int64)
			}
			*disk += *r.DiskBytes
		}
	}
	cores := runtime.NumCPU()
	label := ui.Muted()
	value := ui.FG(theme.Current.TextEmphasis).Bold(true)
	line := label.Render("CPU ") + ui.Meter(cpu/float64(cores*100), lay.meter, cpuStyle(cpu/float64(cores))) +
		" " + value.Render(fmt.Sprintf("%.1f%%", cpu)) + label.Render(fmt.Sprintf(" of %d cores", cores)) +
		label.Render("    MEM ") + value.Render(ui.FormatBytes(rss)) +
		label.Render("    DISK ") + value.Render(formatOptional(disk, ui.FormatBytes, "—")) +
		label.Render("    ") + value.Render(fmt.Sprintf("%d", procs)) + label.Render(" procs")
	return ui.Clamp(line, width)
}

type columns struct {
	glyph, name, status, cpuBar, cpu, memBar, mem, procs, disk, trend []ui.Span
}

func (lay layout) row(c columns, width int) string {
	cells := []ui.Cell{{Spans: c.glyph, Width: 2}, {Spans: c.name}, {Spans: c.status, Width: 12}}
	if lay.bar > 0 {
		cells = append(cells, ui.Cell{Spans: c.cpuBar, Width: lay.bar + 1})
	}
	cells = append(cells, ui.Cell{Spans: c.cpu, Width: 8, Align: ui.AlignRight}, ui.Cell{Width: 2})
	if lay.bar > 0 {
		cells = append(cells, ui.Cell{Spans: c.memBar, Width: lay.bar + 1})
	}
	cells = append(cells, ui.Cell{Spans: c.mem, Width: 10, Align: ui.AlignRight}, ui.Cell{Spans: c.procs, Width: 7, Align: ui.AlignRight}, ui.Cell{Spans: c.disk, Width: 10, Align: ui.AlignRight})
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

func (m Model) headerRow(width int, lay layout) string {
	t := theme.Current
	plain := lipgloss.NewStyle().Foreground(t.TextMuted).Bold(true)
	active := lipgloss.NewStyle().Foreground(t.Primary).Bold(true)
	head := func(title string, by Sort) []ui.Span {
		if by != "" && by == m.sortBy {
			return span(title+" ▼", active)
		}
		return span(title, plain)
	}
	return lay.row(columns{
		name:   head("SESSION", SortName),
		status: head("STATUS", ""),
		cpu:    head("CPU", SortCPU),
		mem:    head("MEM", SortMem),
		procs:  head("PROCS", ""),
		disk:   head("DISK", SortDisk),
		trend:  head("TREND", ""),
	}, width)
}

func (m Model) sessionRow(r session.SessionResources, maxRSS int64, width int, lay layout) string {
	st := stylesFor(r.SessionID == nil, r.CPUPercent)
	memFrac := 0.0
	if maxRSS > 0 {
		memFrac = float64(r.RSSBytes) / float64(maxRSS)
	}
	return lay.row(columns{
		glyph:  []ui.Span{ui.SessionGlyph(session.SessionStatus(r.Status))},
		name:   span(r.Name, st.name),
		status: span(r.Status, ui.Muted()),
		cpuBar: span(ui.Meter(r.CPUPercent/100, lay.bar, st.cpu), lipgloss.NewStyle()),
		cpu:    span(fmt.Sprintf("%.1f%%", r.CPUPercent), st.cpu),
		memBar: span(ui.Meter(memFrac, lay.bar, st.mem), lipgloss.NewStyle()),
		mem:    span(ui.FormatBytes(r.RSSBytes), st.name),
		procs:  span(fmt.Sprintf("%d", r.Procs), ui.Muted()),
		disk:   span(formatOptional(r.DiskBytes, ui.FormatBytes, "—"), st.name),
		trend:  span(ui.Sparkline(m.history[r.MuxSession], lay.trend), st.cpu),
	}, width)
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

func sortProcesses(procs []mux.ProcessUsage, by Sort) []mux.ProcessUsage {
	out := append([]mux.ProcessUsage(nil), procs...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch by {
		case SortMem:
			if a.RSSBytes != b.RSSBytes {
				return a.RSSBytes > b.RSSBytes
			}
		case SortName:
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

func (m Model) processRows(r session.SessionResources, width int, lay layout) []string {
	procs := sortProcesses(r.Processes, m.sortBy)
	hidden := 0
	if len(procs) > procsPerSession {
		hidden = len(procs) - procsPerSession
		procs = procs[:procsPerSession]
	}
	tree := ui.FG(theme.Current.SurfaceVariant)

	var lines []string
	for i, p := range procs {
		branch := "├ "
		if i == len(procs)-1 && hidden == 0 {
			branch = "└ "
		}
		st := stylesFor(r.SessionID == nil, p.CPUPercent)
		lines = append(lines, lay.row(columns{
			name:  []ui.Span{{Text: branch, Style: tree}, {Text: p.Command, Style: st.name}},
			cpu:   span(fmt.Sprintf("%.1f%%", p.CPUPercent), st.cpu),
			mem:   span(ui.FormatBytes(p.RSSBytes), ui.Muted()),
			procs: span(fmt.Sprintf("%d", p.PID), ui.Muted()),
		}, width))
	}
	if hidden > 0 {
		lines = append(lines, lay.row(columns{
			name: []ui.Span{{Text: "└ ", Style: tree}, {Text: fmt.Sprintf("+%d more", hidden), Style: ui.Muted().Italic(true)}},
		}, width))
	}
	return lines
}
