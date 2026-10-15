package mux

import (
	"context"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Usage struct {
	Session    string         `json:"session"`
	CPUPercent float64        `json:"cpu_pct"`
	RSSBytes   int64          `json:"rss_bytes"`
	Procs      int            `json:"procs"`
	Processes  []ProcessUsage `json:"processes"`
}

type ProcessUsage struct {
	PID        int     `json:"pid"`
	Command    string  `json:"command"`
	CPUPercent float64 `json:"cpu_pct"`
	RSSBytes   int64   `json:"rss_bytes"`
}

const maxCPUDeltaWindow = time.Minute

type cpuSample struct {
	cpuTime time.Duration
	at      time.Time
}

type resourceSampler struct {
	lister ProcessLister
	driver Driver
	now    func() time.Time
	mu     sync.Mutex
	prev   map[int]cpuSample
}

func newResourceSampler(lister ProcessLister, driver Driver) *resourceSampler {
	return &resourceSampler{lister: lister, driver: driver, now: time.Now, prev: make(map[int]cpuSample)}
}

func (s *resourceSampler) sample(ctx context.Context) ([]Usage, error) {
	procs, err := s.lister.ListProcesses(ctx)
	if err != nil {
		return nil, err
	}
	shells, err := s.driver.PaneShells(procs)
	if err != nil {
		return nil, err
	}

	now := s.now()
	s.mu.Lock()
	cpu := make(map[int]float64, len(procs))
	next := make(map[int]cpuSample, len(procs))
	for _, p := range procs {
		cpu[p.PID] = p.CPUPercent
		if prev, ok := s.prev[p.PID]; ok && p.CPUTime >= prev.cpuTime && now.After(prev.at) && now.Sub(prev.at) <= maxCPUDeltaWindow {
			cpu[p.PID] = 100 * float64(p.CPUTime-prev.cpuTime) / float64(now.Sub(prev.at))
		}
		next[p.PID] = cpuSample{cpuTime: p.CPUTime, at: now}
	}
	s.prev = next
	s.mu.Unlock()

	byName := make(map[string]*Usage)
	for _, p := range attribute(procs, shells) {
		u := byName[p.session]
		if u == nil {
			u = &Usage{Session: p.session}
			byName[p.session] = u
		}
		u.CPUPercent += cpu[p.proc.PID]
		u.RSSBytes += p.proc.RSSBytes
		u.Procs++
		u.Processes = append(u.Processes, ProcessUsage{
			PID:        p.proc.PID,
			Command:    commandName(p.proc.Command),
			CPUPercent: cpu[p.proc.PID],
			RSSBytes:   p.proc.RSSBytes,
		})
	}

	usages := make([]Usage, 0, len(byName))
	for _, u := range byName {
		sort.Slice(u.Processes, func(i, j int) bool {
			a, b := u.Processes[i], u.Processes[j]
			if a.CPUPercent != b.CPUPercent {
				return a.CPUPercent > b.CPUPercent
			}
			if a.RSSBytes != b.RSSBytes {
				return a.RSSBytes > b.RSSBytes
			}
			return a.PID < b.PID
		})
		usages = append(usages, *u)
	}
	sort.Slice(usages, func(i, j int) bool { return usages[i].Session < usages[j].Session })
	return usages, nil
}

type attributed struct {
	proc    Process
	session string
}

func attribute(procs []Process, shells map[int]string) []attributed {
	parent := make(map[int]int, len(procs))
	for _, p := range procs {
		parent[p.PID] = p.PPID
	}
	owner := make(map[int]string, len(procs))
	var resolve func(pid int, depth int) string
	resolve = func(pid int, depth int) string {
		if name, ok := shells[pid]; ok {
			return name
		}
		if name, ok := owner[pid]; ok {
			return name
		}
		ppid, ok := parent[pid]
		if !ok || ppid == pid || ppid <= 1 || depth > len(procs) {
			return ""
		}
		name := resolve(ppid, depth+1)
		owner[pid] = name
		return name
	}

	var out []attributed
	for _, p := range procs {
		if name := resolve(p.PID, 0); name != "" {
			out = append(out, attributed{proc: p, session: name})
		}
	}
	return out
}

func commandName(command string) string {
	if command == "" {
		return ""
	}
	return filepath.Base(command)
}
