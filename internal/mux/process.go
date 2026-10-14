package mux

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Process struct {
	PID        int
	PPID       int
	RSSBytes   int64
	CPUTime    time.Duration
	CPUPercent float64
	Command    string
}

type ProcessLister interface {
	ListProcesses(ctx context.Context) ([]Process, error)
}

type PSLister struct{}

func (PSLister) ListProcesses(ctx context.Context) ([]Process, error) {
	out, err := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid=,ppid=,rss=,time=,%cpu=,comm=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	return parsePS(out)
}

func parsePS(out []byte) ([]Process, error) {
	var procs []Process
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			return nil, fmt.Errorf("ps: malformed line %q", line)
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			return nil, fmt.Errorf("ps: pid in %q: %w", line, err)
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("ps: ppid in %q: %w", line, err)
		}
		rssKiB, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("ps: rss in %q: %w", line, err)
		}
		cpuTime, err := parseCPUTime(fields[3])
		if err != nil {
			return nil, fmt.Errorf("ps: time in %q: %w", line, err)
		}
		pct, err := strconv.ParseFloat(fields[4], 64)
		if err != nil {
			return nil, fmt.Errorf("ps: %%cpu in %q: %w", line, err)
		}
		procs = append(procs, Process{
			PID:        pid,
			PPID:       ppid,
			RSSBytes:   rssKiB * 1024,
			CPUTime:    cpuTime,
			CPUPercent: pct,
			Command:    strings.Join(fields[5:], " "),
		})
	}
	return procs, scanner.Err()
}

func parseCPUTime(s string) (time.Duration, error) {
	var days int
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil {
			return 0, err
		}
		days, s = n, rest
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("unrecognised cpu time %q", s)
	}
	secs, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil {
		return 0, err
	}
	mins, err := strconv.Atoi(parts[len(parts)-2])
	if err != nil {
		return 0, err
	}
	hours := 0
	if len(parts) == 3 {
		if hours, err = strconv.Atoi(parts[0]); err != nil {
			return 0, err
		}
	}
	total := time.Duration(days)*24*time.Hour + time.Duration(hours)*time.Hour + time.Duration(mins)*time.Minute
	return total + time.Duration(secs*float64(time.Second)), nil
}
