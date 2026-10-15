package mux

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func procTable(cpuSecs map[int]float64) []Process {
	p := func(pid, ppid int, rssKiB int64, pct float64, cmd string) Process {
		return Process{
			PID: pid, PPID: ppid, RSSBytes: rssKiB * 1024, CPUPercent: pct, Command: cmd,
			CPUTime: time.Duration(cpuSecs[pid] * float64(time.Second)),
		}
	}
	return []Process{
		p(1, 0, 10, 0, "/sbin/launchd"),
		p(100, 1, 80, 1, "/opt/homebrew/bin/tuios"),
		p(101, 100, 2, 0, "/bin/zsh"),
		p(102, 101, 600, 13, "claude"),
		p(103, 102, 4, 50, "/bin/zsh"),
		p(104, 103, 300, 90, "go"),
		p(201, 100, 2, 0, "/bin/zsh"),
		p(202, 201, 50, 2, "nvim"),
		p(301, 100, 2, 0, "/bin/zsh"),
		p(500, 1, 999, 70, "Safari"),
	}
}

func newSamplerForTest(procs []Process) (*resourceSampler, *FakeProcessLister, *FakeDriver, *time.Time) {
	lister := &FakeProcessLister{}
	lister.SetProcesses(procs)
	driver := NewFakeDriver()
	driver.SetPaneShell(101, "alpha")
	driver.SetPaneShell(201, "beta")
	driver.SetPaneShell(301, "alpha")
	s := newResourceSampler(lister, driver)
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }
	return s, lister, driver, &now
}

func TestResourceSampler_AttributesSubtreesToPaneSessions(t *testing.T) {
	s, _, _, _ := newSamplerForTest(procTable(nil))

	usages, err := s.sample(context.Background())
	require.NoError(t, err)

	assert.Equal(t, []Usage{
		{Session: "alpha", CPUPercent: 153, RSSBytes: (2 + 600 + 4 + 300 + 2) * 1024, Procs: 5},
		{Session: "beta", CPUPercent: 2, RSSBytes: (2 + 50) * 1024, Procs: 2},
	}, totalsOnly(usages))
}

func TestResourceSampler_ListsSessionProcessesInProcessTableOrder(t *testing.T) {
	s, _, _, _ := newSamplerForTest(procTable(nil))

	usages, err := s.sample(context.Background())
	require.NoError(t, err)

	assert.Equal(t, []ProcessUsage{
		{PID: 101, Command: "zsh", CPUPercent: 0, RSSBytes: 2 * 1024},
		{PID: 102, Command: "claude", CPUPercent: 13, RSSBytes: 600 * 1024},
		{PID: 103, Command: "zsh", CPUPercent: 50, RSSBytes: 4 * 1024},
		{PID: 104, Command: "go", CPUPercent: 90, RSSBytes: 300 * 1024},
		{PID: 301, Command: "zsh", CPUPercent: 0, RSSBytes: 2 * 1024},
	}, usages[0].Processes)
	assert.Equal(t, []ProcessUsage{
		{PID: 201, Command: "zsh", CPUPercent: 0, RSSBytes: 2 * 1024},
		{PID: 202, Command: "nvim", CPUPercent: 2, RSSBytes: 50 * 1024},
	}, usages[1].Processes)
}

func totalsOnly(usages []Usage) []Usage {
	out := make([]Usage, len(usages))
	for i, u := range usages {
		u.Processes = nil
		out[i] = u
	}
	return out
}

func TestResourceSampler_SecondSampleUsesCPUTimeDelta(t *testing.T) {
	s, lister, _, now := newSamplerForTest(procTable(map[int]float64{102: 10, 104: 5, 202: 1}))
	_, err := s.sample(context.Background())
	require.NoError(t, err)

	*now = now.Add(2 * time.Second)
	next := procTable(map[int]float64{102: 11, 104: 5.5, 202: 1})
	next = append(next, Process{PID: 105, PPID: 103, CPUPercent: 7})
	lister.SetProcesses(next)

	usages, err := s.sample(context.Background())
	require.NoError(t, err)
	require.Len(t, usages, 2)
	assert.InDelta(t, 50+25+7, usages[0].CPUPercent, 0.001)
	assert.Equal(t, 6, usages[0].Procs)
	assert.InDelta(t, 0, usages[1].CPUPercent, 0.001)
}

func TestResourceSampler_ReusedPIDFallsBackToReportedPercent(t *testing.T) {
	s, lister, _, now := newSamplerForTest(procTable(map[int]float64{202: 100}))
	_, err := s.sample(context.Background())
	require.NoError(t, err)

	*now = now.Add(2 * time.Second)
	lister.SetProcesses(procTable(map[int]float64{202: 1}))
	usages, err := s.sample(context.Background())
	require.NoError(t, err)
	assert.InDelta(t, 2, usages[1].CPUPercent, 0.001)
}

func TestResourceSampler_StalePreviousSampleFallsBackToReportedPercent(t *testing.T) {
	s, lister, _, now := newSamplerForTest(procTable(map[int]float64{202: 1}))
	_, err := s.sample(context.Background())
	require.NoError(t, err)

	*now = now.Add(10 * time.Minute)
	lister.SetProcesses(procTable(map[int]float64{202: 61}))
	usages, err := s.sample(context.Background())
	require.NoError(t, err)
	assert.InDelta(t, 2, usages[1].CPUPercent, 0.001)
}

func TestResourceSampler_SurfacesListerError(t *testing.T) {
	s, lister, _, _ := newSamplerForTest(nil)
	lister.Err = errors.New("ps failed")
	_, err := s.sample(context.Background())
	assert.ErrorContains(t, err, "ps failed")
}

func TestAttribute_ToleratesParentCycles(t *testing.T) {
	procs := []Process{{PID: 10, PPID: 11}, {PID: 11, PPID: 10}, {PID: 12, PPID: 12}}
	assert.Empty(t, attribute(procs, map[int]string{}))
}

func TestService_ResourceUsageUsesInjectedLister(t *testing.T) {
	svc, driver := newTestService(t)
	driver.SetPaneShell(101, "alpha")
	lister := &FakeProcessLister{}
	lister.SetProcesses([]Process{{PID: 101, PPID: 100, RSSBytes: 1024}, {PID: 100, PPID: 1, Command: "tuios"}})
	svc.SetProcessLister(lister)

	usages, err := svc.ResourceUsage(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []Usage{{Session: "alpha", RSSBytes: 1024, Procs: 1, Processes: []ProcessUsage{{PID: 101, Command: "?", RSSBytes: 1024}}}}, usages)
}
