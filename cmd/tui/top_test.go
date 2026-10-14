package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eleonorayaya/utena/internal/session"
)

func topRows() []session.SessionResources {
	id := uint(7)
	return []session.SessionResources{
		{SessionID: &id, Name: "alpha", Status: "active", CPUPercent: 12, RSSBytes: 3 << 30, Procs: 9},
		{Name: "scratch", Status: session.ResourceStatusUnmanaged, CPUPercent: 80, RSSBytes: 10 << 20, Procs: 2},
		{Name: "beta", Status: "inactive", CPUPercent: 12, RSSBytes: 1 << 20, Procs: 1},
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
	assert.Equal(t, []string{"scratch", "alpha", "beta"}, names(sortResources(rows, sortCPU)))
	assert.Equal(t, []string{"alpha", "scratch", "beta"}, names(sortResources(rows, sortMem)))
	assert.Equal(t, []string{"alpha", "beta", "scratch"}, names(sortResources(rows, sortName)))
	assert.Equal(t, []string{"alpha", "scratch", "beta"}, names(rows))
}

func TestFormatBytes(t *testing.T) {
	assert.Equal(t, "512B", formatBytes(512))
	assert.Equal(t, "1.5KiB", formatBytes(1536))
	assert.Equal(t, "10.0MiB", formatBytes(10<<20))
	assert.Equal(t, "3.0GiB", formatBytes(3<<30))
}

func TestWriteResourcesTSV(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeResourcesTSV(&buf, sortResources(topRows(), sortCPU)))
	assert.Equal(t, "name\tstatus\tcpu_pct\trss_bytes\tprocs\n"+
		"scratch\tunmanaged\t80.0\t10485760\t2\n"+
		"alpha\tactive\t12.0\t3221225472\t9\n"+
		"beta\tinactive\t12.0\t1048576\t1\n", buf.String())
}

func TestTopModel_FetchRenderSortAndQuit(t *testing.T) {
	sampled := time.Date(2026, 10, 14, 9, 30, 0, 0, time.Local)
	m := newTopModel("0", time.Second)
	m.fetch = func() (*session.SessionResourcesResponse, error) {
		return &session.SessionResourcesResponse{Sessions: topRows(), SampledAt: sampled}, nil
	}

	msg := m.Init()()
	next, cmd := m.Update(msg)
	m = next.(topModel)
	require.NotNil(t, cmd)

	view := m.View()
	assert.Contains(t, view, "sampled 09:30:00")
	assert.Contains(t, view, "sort: cpu")
	assert.Less(t, strings.Index(view, "scratch"), strings.Index(view, "alpha"))
	assert.Contains(t, view, "3.0GiB")
	assert.Contains(t, view, "TOTAL")

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = next.(topModel)
	view = m.View()
	assert.Contains(t, view, "sort: mem")
	assert.Less(t, strings.Index(view, "alpha"), strings.Index(view, "scratch"))

	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	require.NotNil(t, cmd)
	assert.Equal(t, tea.Quit(), cmd())
}

func TestTopModel_KeepsLastRowsOnError(t *testing.T) {
	m := newTopModel("0", time.Second)
	next, _ := m.Update(resourcesMsg{resp: &session.SessionResourcesResponse{Sessions: topRows(), SampledAt: time.Now()}})
	next, cmd := next.(topModel).Update(resourcesMsg{err: errors.New("daemon down")})
	m = next.(topModel)
	require.NotNil(t, cmd)

	view := m.View()
	assert.Contains(t, view, "alpha")
	assert.Contains(t, view, "daemon down")
}
