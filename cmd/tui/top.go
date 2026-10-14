package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/theme"
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

type resourcesMsg struct {
	resp *session.SessionResourcesResponse
	err  error
}

type topTickMsg struct{}

type topModel struct {
	port      string
	interval  time.Duration
	fetch     func() (*session.SessionResourcesResponse, error)
	rows      []session.SessionResources
	sampledAt time.Time
	err       error
	sortBy    topSort
	width     int
}

func newTopModel(port string, interval time.Duration) topModel {
	return topModel{
		port:     port,
		interval: interval,
		sortBy:   sortCPU,
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
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "c":
			m.sortBy = sortCPU
		case "m":
			m.sortBy = sortMem
		case "n":
			m.sortBy = sortName
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case resourcesMsg:
		m.err = msg.err
		if msg.err == nil {
			m.rows = msg.resp.Sessions
			m.sampledAt = msg.resp.SampledAt
		}
		return m, tea.Tick(m.interval, func(time.Time) tea.Msg { return topTickMsg{} })
	case topTickMsg:
		return m, m.load()
	}
	return m, nil
}

func (m topModel) View() string {
	t := theme.Current
	title := lipgloss.NewStyle().Bold(true).Foreground(t.Primary)
	muted := lipgloss.NewStyle().Foreground(t.TextMuted)
	header := lipgloss.NewStyle().Bold(true).Foreground(t.TextEmphasis)
	errStyle := lipgloss.NewStyle().Foreground(t.Error)

	var b strings.Builder
	sampled := "waiting for first sample"
	if !m.sampledAt.IsZero() {
		sampled = "sampled " + m.sampledAt.Local().Format("15:04:05")
	}
	b.WriteString(title.Render("utena top") + muted.Render(fmt.Sprintf("  %s · sort: %s", sampled, m.sortBy)) + "\n\n")

	nameWidth := 24
	for _, r := range m.rows {
		nameWidth = max(nameWidth, lipgloss.Width(r.Name))
	}
	if m.width > 0 {
		nameWidth = min(nameWidth, max(12, m.width-46))
	}
	row := func(name, status, cpu, mem, procs string) string {
		return fmt.Sprintf("%-*s  %-10s %8s %10s %6s", nameWidth, truncate(name, nameWidth), truncate(status, 10), cpu, mem, procs)
	}
	b.WriteString(header.Render(row("SESSION", "STATUS", "CPU%", "MEM", "PROCS")) + "\n")

	var totalCPU float64
	var totalRSS int64
	var totalProcs int
	for _, r := range sortResources(m.rows, m.sortBy) {
		line := row(r.Name, r.Status, fmt.Sprintf("%.1f", r.CPUPercent), formatBytes(r.RSSBytes), fmt.Sprintf("%d", r.Procs))
		if r.SessionID == nil {
			line = muted.Render(line)
		}
		b.WriteString(line + "\n")
		totalCPU += r.CPUPercent
		totalRSS += r.RSSBytes
		totalProcs += r.Procs
	}
	if len(m.rows) == 0 && m.err == nil && !m.sampledAt.IsZero() {
		b.WriteString(muted.Render("no session processes found") + "\n")
	}
	b.WriteString(header.Render(row("TOTAL", "", fmt.Sprintf("%.1f", totalCPU), formatBytes(totalRSS), fmt.Sprintf("%d", totalProcs))) + "\n")

	if m.err != nil {
		b.WriteString("\n" + errStyle.Render(m.err.Error()) + "\n")
	}
	b.WriteString("\n" + muted.Render("c cpu · m mem · n name · q quit"))
	return b.String()
}

func truncate(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}
