package mux

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCPUTime(t *testing.T) {
	cases := map[string]time.Duration{
		"0:00.04":     40 * time.Millisecond,
		"2:57.80":     2*time.Minute + 57800*time.Millisecond,
		"21:22.32":    21*time.Minute + 22320*time.Millisecond,
		"1234:56.78":  1234*time.Minute + 56780*time.Millisecond,
		"01:02:03":    time.Hour + 2*time.Minute + 3*time.Second,
		"2-03:04:05":  51*time.Hour + 4*time.Minute + 5*time.Second,
		"00:00:00.50": 500 * time.Millisecond,
	}
	for in, want := range cases {
		got, err := parseCPUTime(in)
		require.NoError(t, err, in)
		assert.InDelta(t, float64(want), float64(got), float64(time.Microsecond), in)
	}

	for _, bad := range []string{"", "12", "a:b", "1:2:3:4", "x-01:02:03"} {
		_, err := parseCPUTime(bad)
		assert.Error(t, err, bad)
	}
}

func TestParsePS(t *testing.T) {
	out := []byte(`    1     0  15232   1:23.45   0.0 /sbin/launchd
46858     1  80752   2:57.80   0.6 /opt/homebrew/bin/tuios
74938 46867 617216  21:22.32  13.0 claude
  900   800      4   0:00.00   0.0 /Applications/Some App.app/Contents/MacOS/Some App
`)
	procs, err := parsePS(out)
	require.NoError(t, err)
	require.Len(t, procs, 4)
	assert.Equal(t, Process{
		PID: 74938, PPID: 46867, RSSBytes: 617216 * 1024,
		CPUTime: 21*time.Minute + 22320*time.Millisecond, CPUPercent: 13.0, Command: "claude",
	}, procs[2])
	assert.Equal(t, "/Applications/Some App.app/Contents/MacOS/Some App", procs[3].Command)

	_, err = parsePS([]byte("1 0 12\n"))
	assert.Error(t, err)
}
