package coder

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConn_EnsureAddsHostAndReusesConnection(t *testing.T) {
	h := newHarness(t)
	f1, err := h.conns.Ensure(context.Background(), "ws")
	require.NoError(t, err)
	f2, err := h.conns.Ensure(context.Background(), "ws")
	require.NoError(t, err)
	assert.Equal(t, f1.baseURL, f2.baseURL)
	assert.Equal(t, fmt.Sprintf("http://127.0.0.1:%d", h.follower.port()), f1.baseURL)
	assert.Equal(t, []string{"tuios hosts add ws ws.coder"}, h.runner.Runs())
	assert.Equal(t, 1, h.runner.StartCount())
}

func TestConn_ForwardFailureRetriesWithFreshPort(t *testing.T) {
	h := newHarness(t)
	dead, live := deadPort(t), h.follower.port()
	ports := []int{dead, live}
	h.conns.ports = func() (int, error) {
		p := ports[0]
		ports = ports[1:]
		return p, nil
	}
	h.runner.onStart = func(n int, p *fakeProcess) {
		if n == 1 {
			p.Exit(errors.New("remote port forwarding failed"))
		}
	}

	_, err := h.conns.Ensure(context.Background(), "ws")
	require.NoError(t, err)
	starts := h.runner.Starts()
	require.Len(t, starts, 2)
	assert.Contains(t, starts[0].String(), fmt.Sprintf("-L 127.0.0.1:%d:", dead))
	assert.Contains(t, starts[1].String(), fmt.Sprintf("-L 127.0.0.1:%d:", live))
}

func TestConn_EnsureFailureRemovesHost(t *testing.T) {
	h := newHarness(t)
	h.runner.startErr = errors.New("ssh missing")
	_, err := h.conns.Ensure(context.Background(), "ws")
	require.ErrorContains(t, err, "ssh missing")
	assert.Equal(t, []string{"tuios hosts add ws ws.coder", "tuios hosts remove ws"}, h.runner.Runs())
	_, connected := h.conns.Connected("ws")
	assert.False(t, connected)
}

func TestConn_RestartsDeadForwardWithBackoff(t *testing.T) {
	h := newHarness(t)
	_, err := h.conns.Ensure(context.Background(), "ws")
	require.NoError(t, err)

	h.runner.Proc(0).Exit(errors.New("connection reset"))
	require.Eventually(t, func() bool {
		_, up := h.conns.Connected("ws")
		return up && h.runner.StartCount() == 2
	}, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, []time.Duration{time.Second}, h.sleeper.Waits())
	assert.Equal(t, []string{"tuios hosts add ws ws.coder"}, h.runner.Runs(), "the host entry is kept across restarts")
}

func TestConn_BackoffGrowsWhileReconnectFails(t *testing.T) {
	h := newHarness(t)
	_, err := h.conns.Ensure(context.Background(), "ws")
	require.NoError(t, err)

	dead, live := deadPort(t), h.follower.port()
	failing := 2 * connectAttempts
	var mu sync.Mutex
	allocated := 0
	h.conns.ports = func() (int, error) {
		mu.Lock()
		defer mu.Unlock()
		allocated++
		if allocated <= failing {
			return dead, nil
		}
		return live, nil
	}
	h.runner.mu.Lock()
	h.runner.onStart = func(n int, p *fakeProcess) {
		if n > 1 && n <= 1+failing {
			p.Exit(errors.New("unreachable"))
		}
	}
	h.runner.mu.Unlock()

	h.runner.Proc(0).Exit(errors.New("connection reset"))
	require.Eventually(t, func() bool {
		_, up := h.conns.Connected("ws")
		return up && h.runner.StartCount() == 2+failing
	}, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}, h.sleeper.Waits())
	assert.Equal(t, 2+failing, h.runner.StartCount())
}

func TestConn_ReleaseStopsRestarts(t *testing.T) {
	h := newHarness(t)
	_, err := h.conns.Ensure(context.Background(), "ws")
	require.NoError(t, err)
	h.conns.Release("ws")
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 1, h.runner.StartCount())
	assert.True(t, h.runner.Proc(0).killed())
	assert.Equal(t, []string{"tuios hosts add ws ws.coder", "tuios hosts remove ws"}, h.runner.Runs())
}

func TestConn_TickDropsOnlyAfterIdleDisconnect(t *testing.T) {
	h := newHarness(t)
	_, err := h.conns.Ensure(context.Background(), "ws")
	require.NoError(t, err)

	h.clock.Advance(9 * time.Minute)
	h.conns.Tick(func(string) bool { return false })
	_, up := h.conns.Connected("ws")
	assert.True(t, up)

	h.clock.Advance(5 * time.Minute)
	h.conns.Tick(func(string) bool { return true })
	h.clock.Advance(9 * time.Minute)
	h.conns.Tick(func(string) bool { return false })
	_, up = h.conns.Connected("ws")
	assert.True(t, up, "needed resets the idle timer")

	_, err = h.conns.Ensure(context.Background(), "ws")
	require.NoError(t, err)
	h.clock.Advance(9 * time.Minute)
	h.conns.Tick(func(string) bool { return false })
	_, up = h.conns.Connected("ws")
	assert.True(t, up, "Ensure resets the idle timer")

	h.clock.Advance(time.Minute)
	h.conns.Tick(func(string) bool { return false })
	_, up = h.conns.Connected("ws")
	assert.False(t, up)
}

func TestConn_CloseKillsForwardsAndRemovesHosts(t *testing.T) {
	h := newHarness(t)
	_, err := h.conns.Ensure(context.Background(), "a")
	require.NoError(t, err)
	_, err = h.conns.Ensure(context.Background(), "b")
	require.NoError(t, err)
	h.conns.Close()
	assert.True(t, h.runner.Proc(0).killed())
	assert.True(t, h.runner.Proc(1).killed())
	assert.Contains(t, h.runner.Runs(), "tuios hosts remove a")
	assert.Contains(t, h.runner.Runs(), "tuios hosts remove b")
	assert.Empty(t, h.conns.Workspaces())
}

func TestConn_EnsureAfterCloseStartsNothing(t *testing.T) {
	h := newHarness(t)
	h.conns.Close()
	_, err := h.conns.Ensure(context.Background(), "ws")
	assert.ErrorIs(t, err, ErrClosed)
	assert.Equal(t, 0, h.runner.StartCount())
	assert.Empty(t, h.runner.Runs())
}
