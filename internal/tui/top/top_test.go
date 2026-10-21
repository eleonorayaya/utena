package top

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eleonorayaya/utena/internal/mux"
	"github.com/eleonorayaya/utena/internal/session"
)

func topRows() []session.SessionResources {
	id := uint(7)
	disk := int64(5 << 30)
	small := int64(2 << 20)
	return []session.SessionResources{
		{SessionID: &id, Name: "alpha", MuxSession: "alpha", Status: "active", CPUPercent: 12, RSSBytes: 3 << 30, Procs: 9, DiskBytes: &small},
		{Name: "scratch", MuxSession: "scratch", Status: session.ResourceStatusUnmanaged, CPUPercent: 80, RSSBytes: 10 << 20, Procs: 2},
		{Name: "beta", MuxSession: "beta", Status: "inactive", CPUPercent: 12, RSSBytes: 1 << 20, Procs: 1, DiskBytes: &disk},
	}
}

func names(rows []session.SessionResources) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Name
	}
	return out
}

func TestSortResources(t *testing.T) {
	rows := topRows()
	assert.Equal(t, []string{"scratch", "alpha", "beta"}, names(SortResources(rows, SortCPU)))
	assert.Equal(t, []string{"alpha", "scratch", "beta"}, names(SortResources(rows, SortMem)))
	assert.Equal(t, []string{"alpha", "beta", "scratch"}, names(SortResources(rows, SortName)))
	assert.Equal(t, []string{"beta", "alpha", "scratch"}, names(SortResources(rows, SortDisk)))
	assert.Equal(t, []string{"alpha", "scratch", "beta"}, names(rows))
}

func TestWriteResourcesTSV(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, WriteTSV(&buf, SortResources(topRows(), SortCPU)))
	assert.Equal(t, "name\tstatus\tcpu_pct\trss_bytes\tprocs\tdisk_bytes\n"+
		"scratch\tunmanaged\t80.0\t10485760\t2\t-\n"+
		"alpha\tactive\t12.0\t3221225472\t9\t2097152\n"+
		"beta\tinactive\t12.0\t1048576\t1\t5368709120\n", buf.String())
}

func TestTopModel_FetchRenderSortAndQuit(t *testing.T) {
	sampled := time.Date(2026, 10, 14, 9, 30, 0, 0, time.Local)
	m := New(time.Second, nil)
	m.fetch = func() (*session.SessionResourcesResponse, error) {
		return &session.SessionResourcesResponse{Sessions: topRows(), SampledAt: sampled}, nil
	}

	msg := m.Init()()
	next, cmd := m.Update(msg)
	m = next.(Model)
	require.NotNil(t, cmd)

	view := m.View()
	assert.Contains(t, view, "09:30:00")
	assert.Contains(t, view, "CPU ▼")
	assert.Less(t, strings.Index(view, "scratch"), strings.Index(view, "alpha"))
	assert.Contains(t, view, "3.0GiB")
	assert.Contains(t, view, "12 procs")
	assert.Contains(t, view, "DISK 5.0GiB")
	assert.Contains(t, view, "2.0MiB")

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = next.(Model)
	view = m.View()
	assert.Contains(t, view, "MEM ▼")
	assert.NotContains(t, view, "CPU ▼")
	assert.Less(t, strings.Index(view, "alpha"), strings.Index(view, "scratch"))

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = next.(Model)
	view = ansi.Strip(m.View())
	assert.Contains(t, view, "DISK ▼")
	assert.Less(t, strings.Index(view, "beta"), strings.Index(view, "alpha"))

	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	require.NotNil(t, cmd)
	assert.Equal(t, tea.Quit(), cmd())
}

func TestTopModel_KeepsLastRowsOnError(t *testing.T) {
	m := New(time.Second, nil)
	next, _ := m.Update(loaded(topRows()))
	next, cmd := next.(Model).Update(resourcesMsg{err: errors.New("daemon down")})
	m = next.(Model)
	require.NotNil(t, cmd)

	view := m.View()
	assert.Contains(t, view, "alpha")
	assert.Contains(t, view, "daemon down")
}

func TestTopModel_HistoryFollowsSessionsAndIsBounded(t *testing.T) {
	m := New(time.Second, nil)
	for i := 0; i < historyLen+5; i++ {
		next, _ := m.Update(loaded(topRows()))
		m = next.(Model)
	}
	assert.Len(t, m.history["alpha"], historyLen)

	next, _ := m.Update(loaded(topRows()[:1]))
	m = next.(Model)
	assert.NotContains(t, m.history, "beta")
}

func TestTopModel_NarrowWidthDropsBarsAndTrend(t *testing.T) {
	m := New(time.Second, nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 70, Height: 20})
	next, _ = next.(Model).Update(loaded(topRows()))
	view := next.(Model).View()
	assert.NotContains(t, view, "TREND")
	assert.Contains(t, view, "DISK")
	for _, line := range strings.Split(view, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(line), 70, line)
	}
}

func procRows() []session.SessionResources {
	id := uint(7)
	return []session.SessionResources{{
		SessionID: &id, Name: "alpha", MuxSession: "alpha", Status: "active", CPUPercent: 60, RSSBytes: 900 << 20, Procs: 6,
		Processes: []mux.ProcessUsage{
			{PID: 11, Command: "zsh", CPUPercent: 0, RSSBytes: 2 << 20},
			{PID: 12, Command: "claude", CPUPercent: 40, RSSBytes: 600 << 20},
			{PID: 13, Command: "go", CPUPercent: 15, RSSBytes: 200 << 20},
			{PID: 14, Command: "gopls", CPUPercent: 5, RSSBytes: 90 << 20},
			{PID: 15, Command: "node", CPUPercent: 0, RSSBytes: 8 << 20},
			{PID: 16, Command: "cat", CPUPercent: 0, RSSBytes: 1 << 20},
		},
	}}
}

func TestTopModel_ShowsBusiestProcessesUnderSessions(t *testing.T) {
	m := New(time.Second, nil)
	next, _ := m.Update(loaded(procRows()))
	assert.NotContains(t, ansi.Strip(next.(Model).View()), "claude")
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = next.(Model)

	view := ansi.Strip(m.View())
	assert.Contains(t, view, "├ claude")
	assert.Less(t, strings.Index(view, "claude"), strings.Index(view, "├ go"))
	assert.Less(t, strings.Index(view, "├ go"), strings.Index(view, "gopls"))
	assert.Contains(t, view, "└ +2 more")
	assert.NotContains(t, view, "├ cat")

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	view = ansi.Strip(next.(Model).View())
	assert.Less(t, strings.Index(view, "├ claude"), strings.Index(view, "├ go"))
	assert.Less(t, strings.Index(view, "gopls"), strings.Index(view, "node"))

	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	view = ansi.Strip(next.(Model).View())
	assert.NotContains(t, view, "claude")
	assert.Contains(t, view, "alpha")
}

func TestSortProcesses(t *testing.T) {
	procs := procRows()[0].Processes
	pids := func(ps []mux.ProcessUsage) []int {
		out := make([]int, len(ps))
		for i, p := range ps {
			out[i] = p.PID
		}
		return out
	}
	assert.Equal(t, []int{12, 13, 14, 15, 11, 16}, pids(sortProcesses(procs, SortCPU)))
	assert.Equal(t, []int{12, 13, 14, 15, 11, 16}, pids(sortProcesses(procs, SortMem)))
	assert.Equal(t, []int{16, 12, 13, 14, 15, 11}, pids(sortProcesses(procs, SortName)))
}

func loaded(rows []session.SessionResources) resourcesMsg {
	return resourcesMsg{resp: &session.SessionResourcesResponse{Sessions: rows, SampledAt: time.Now()}}
}

func TestTopModel_SummaryFitsAt80Columns(t *testing.T) {
	m := New(time.Second, nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	next, _ = next.(Model).Update(loaded(topRows()))
	assert.Contains(t, ansi.Strip(next.(Model).View()), "12 procs")
}

func TestTopModel_UnknownDiskTotalRendersDash(t *testing.T) {
	rows := topRows()
	for i := range rows {
		rows[i].DiskBytes = nil
	}
	m := New(time.Second, nil)
	next, _ := m.Update(loaded(rows))
	assert.Contains(t, ansi.Strip(next.(Model).View()), "DISK —")
}
