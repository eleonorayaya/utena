package tuios

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTuiosRequest struct {
	Verb   string         `json:"verb"`
	Params map[string]any `json:"params"`
}

type fakeTuiosDaemon struct {
	mu       sync.Mutex
	requests []fakeTuiosRequest
	replies  map[string]string
	stream   []string
}

func startFakeTuios(t *testing.T, replies map[string]string, stream ...string) (*Driver, *fakeTuiosDaemon) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tuios-test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	sock := filepath.Join(dir, "tuios.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	d := &fakeTuiosDaemon{replies: replies, stream: stream}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go d.serve(conn)
		}
	}()
	return &Driver{socketPath: sock}, d
}

func (d *fakeTuiosDaemon) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		return
	}
	var req fakeTuiosRequest
	_ = json.Unmarshal(scanner.Bytes(), &req)
	d.mu.Lock()
	d.requests = append(d.requests, req)
	reply := d.replies[req.Verb]
	d.mu.Unlock()
	_, _ = conn.Write([]byte(reply + "\n"))
	if req.Verb == "subscribe" {
		for _, line := range d.stream {
			_, _ = conn.Write([]byte(line + "\n"))
		}
	}
}

func (d *fakeTuiosDaemon) requestsFor(verb string) []fakeTuiosRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []fakeTuiosRequest
	for _, req := range d.requests {
		if req.Verb == verb {
			out = append(out, req)
		}
	}
	return out
}

func (d *fakeTuiosDaemon) last() fakeTuiosRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.requests[len(d.requests)-1]
}

func TestDriver_NewSessionInjectsEnvAndToleratesExisting(t *testing.T) {
	r, d := startFakeTuios(t, map[string]string{
		"new-session": `{"id":1,"error":{"code":"session_exists","message":"exists"}}`,
	})
	t.Setenv("SHELL", "/bin/zsh")

	require.NoError(t, r.NewSession("work", "/src", map[string]string{"UTENA_SESSION_ID": "7"}))

	req := d.last()
	assert.Equal(t, "work", req.Params["name"])
	assert.Equal(t, "/src", req.Params["cwd"])
	assert.Equal(t, []any{"env", "UTENA_SESSION_ID=7", "/bin/zsh", "-l"}, req.Params["command"])
}

func TestDriver_NewWindowRunsCommandThroughLoginShell(t *testing.T) {
	r, d := startFakeTuios(t, map[string]string{
		"new-window": `{"id":1,"result":{"type":"window_created"}}`,
	})
	t.Setenv("SHELL", "/bin/zsh")

	require.NoError(t, r.NewWindow("work", "/src", `claude "hi"`, map[string]string{"UTENA_SESSION_ID": "7"}))

	assert.Equal(t, []any{"env", "UTENA_SESSION_ID=7", "/bin/zsh", "-lc", `claude "hi"`}, d.last().Params["command"])
}

func TestDriver_KillMissingSessionIsNotAnError(t *testing.T) {
	r, _ := startFakeTuios(t, map[string]string{
		"kill-session": `{"id":1,"error":{"code":"session_not_found","message":"nope"}}`,
	})
	require.NoError(t, r.KillSession("gone"))
}

func TestDriver_OtherErrorsSurface(t *testing.T) {
	r, _ := startFakeTuios(t, map[string]string{
		"kill-session": `{"id":1,"error":{"code":"internal","message":"boom"}}`,
	})
	err := r.KillSession("x")
	require.Error(t, err)
	assert.True(t, isTuiosCode(err, "internal"))
}

func TestDriver_ListWindowsAndHasSession(t *testing.T) {
	r, _ := startFakeTuios(t, map[string]string{
		"list-windows":  `{"id":1,"result":{"windows":[{"index":0,"display_name":"zsh","focused":false},{"index":1,"display_name":"claude","focused":true}]}}`,
		"list-sessions": `{"id":1,"result":{"sessions":[{"name":"work"}]}}`,
	})

	windows, err := r.ListWindows("work")
	require.NoError(t, err)
	assert.Equal(t, []mux.Window{{Index: 0, Name: "zsh"}, {Index: 1, Name: "claude", Active: true}}, windows)

	assert.True(t, r.HasSession("work"))
	assert.False(t, r.HasSession("other"))
}

func TestDriver_DaemonDownIsAnErrorNotAStart(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "tuios-test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "tuios.sock")
	r := &Driver{socketPath: sock}

	err = r.NewSession("work", "/src", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "utena attach")
	assert.NoFileExists(t, sock)
}

func TestCloseWindow_TargetsSessionAndWindowAndToleratesGone(t *testing.T) {
	r, d := startFakeTuios(t, map[string]string{
		"close-window": `{"id":1,"error":{"code":"window_not_found","message":"gone"}}`,
	})
	t.Setenv("TUIOS_SOCKET", r.socketPath)

	require.NoError(t, CloseWindow("work", "abc"))
	req := d.last()
	assert.Equal(t, "close-window", req.Verb)
	assert.Equal(t, "work", req.Params["session"])
	assert.Equal(t, "abc", req.Params["window"])
}

func TestCloseWindow_RefusesEmptyTargets(t *testing.T) {
	require.Error(t, CloseWindow("work", ""))
	require.Error(t, CloseWindow("", "abc"))
}

func TestDriver_ListAgentsAcrossSessions(t *testing.T) {
	r, d := startFakeTuios(t, map[string]string{
		"list-agents": `{"id":1,"result":{"agents":[{"session":"work","window_id":"w1","state":"idle","meta":{"session":"work"}}]}}`,
	})

	agents, err := r.listAgents()
	require.NoError(t, err)
	assert.Equal(t, []agentPane{{Session: "work", WindowID: "w1", State: "idle", Meta: map[string]string{"session": "work"}}}, agents)
	assert.Equal(t, true, d.last().Params["all_sessions"])
}

func TestDriver_SubscribeTranslatesEvents(t *testing.T) {
	r, d := startFakeTuios(t, map[string]string{
		"subscribe":   `{"id":1,"result":{"type":"subscribed"}}`,
		"list-agents": `{"id":1,"result":{"agents":[]}}`,
	},
		`{"type":"session-created","session":"s"}`,
		`{"type":"session-closed","session":"s"}`,
		`{"type":"window-created","session":"s","window":"w1"}`,
		`{"type":"workspace-switched","session":"s"}`,
		`{"type":"gap"}`,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var got []mux.Event
	err := r.Subscribe(ctx, func(ev mux.Event) { got = append(got, ev) })
	require.Error(t, err)

	assert.Equal(t, []mux.Event{
		{Type: mux.EventResync},
		{Type: mux.EventSessionCreated, Session: "s"},
		{Type: mux.EventSessionClosed, Session: "s"},
		{Type: mux.EventWindowsChanged, Session: "s"},
		{Type: mux.EventWindowsChanged, Session: "s"},
		{Type: mux.EventResync},
	}, got)
	assert.Equal(t, []any{
		"session-created", "session-closed", "window-created", "window-closed", "window-retitled",
		"window-focused", "window-moved", "workspace-switched", "agent-state", "gap",
	}, d.requestsFor("subscribe")[0].Params["types"])
	assert.Len(t, d.requestsFor("list-agents"), 2)
}

func TestDriver_AgentStateTagsPaneAndEmitsNothing(t *testing.T) {
	r, d := startFakeTuios(t, map[string]string{
		"set-agent-meta": `{"id":1,"result":{"type":"agent_meta_set"}}`,
	})

	var got []mux.Event
	collect := func(ev mux.Event) { got = append(got, ev) }
	r.translate(tuiosEvent{Type: "agent-state", Session: "work", Window: "w1", State: "working"}, collect)
	r.translate(tuiosEvent{Type: "agent-state", Session: "work", Window: "w2", State: "none"}, collect)

	assert.Empty(t, got)
	writes := d.requestsFor("set-agent-meta")
	require.Len(t, writes, 1)
	assert.Equal(t, map[string]any{
		"session": "work",
		"window":  "w1",
		"source":  "utena",
		"tokens":  map[string]any{"session": "work"},
	}, writes[0].Params)
}

func TestDriver_ResyncTagsUntaggedAgentPanes(t *testing.T) {
	r, d := startFakeTuios(t, map[string]string{
		"subscribe":      `{"id":1,"result":{"type":"subscribed"}}`,
		"list-agents":    `{"id":1,"result":{"agents":[{"session":"work","window_id":"w1","state":"idle","meta":{"session":"work"}},{"session":"other","window_id":"w2","state":"done"}]}}`,
		"set-agent-meta": `{"id":1,"result":{"type":"agent_meta_set"}}`,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var got []mux.Event
	_ = r.Subscribe(ctx, func(ev mux.Event) { got = append(got, ev) })

	assert.Equal(t, []mux.Event{{Type: mux.EventResync}}, got)
	writes := d.requestsFor("set-agent-meta")
	require.Len(t, writes, 1)
	assert.Equal(t, "other", writes[0].Params["session"])
	assert.Equal(t, "w2", writes[0].Params["window"])
	assert.Equal(t, map[string]any{"session": "other"}, writes[0].Params["tokens"])
}

func TestDriver_PaneShellsResolvesTuiosChildrenOnceAndCaches(t *testing.T) {
	r, d := startFakeTuios(t, map[string]string{
		"resolve-pane": `{"id":1,"result":{"by":"pid","pid":11,"session":"work","type":"pane_resolved"}}`,
	})
	procs := []mux.Process{
		{PID: 10, PPID: 1, Command: "/opt/homebrew/bin/tuios"},
		{PID: 11, PPID: 10, Command: "/bin/zsh"},
		{PID: 12, PPID: 11, Command: "claude"},
		{PID: 13, PPID: 1, Command: "Safari"},
	}

	shells, err := r.PaneShells(procs)
	require.NoError(t, err)
	assert.Equal(t, map[int]string{11: "work"}, shells)
	reqs := d.requestsFor("resolve-pane")
	require.Len(t, reqs, 1)
	assert.Equal(t, []any{float64(11)}, reqs[0].Params["pids"])

	shells, err = r.PaneShells(procs)
	require.NoError(t, err)
	assert.Equal(t, map[int]string{11: "work"}, shells)
	assert.Len(t, d.requestsFor("resolve-pane"), 1)
}

func TestDriver_PaneShellsTreatsUnknownPaneAsNotAShell(t *testing.T) {
	r, d := startFakeTuios(t, map[string]string{
		"resolve-pane": `{"id":1,"error":{"code":"window_not_found","message":"no pane"}}`,
	})
	procs := []mux.Process{
		{PID: 10, PPID: 1, Command: "tuios"},
		{PID: 20, PPID: 10, Command: "npm exec ccstatusline"},
	}

	shells, err := r.PaneShells(procs)
	require.NoError(t, err)
	assert.Empty(t, shells)

	_, err = r.PaneShells(procs)
	require.NoError(t, err)
	assert.Len(t, d.requestsFor("resolve-pane"), 1)
}

func TestDriver_PaneShellsSurfacesOtherErrors(t *testing.T) {
	r, _ := startFakeTuios(t, map[string]string{
		"resolve-pane": `{"id":1,"error":{"code":"internal","message":"boom"}}`,
	})
	_, err := r.PaneShells([]mux.Process{{PID: 10, PPID: 1, Command: "tuios"}, {PID: 11, PPID: 10}})
	assert.ErrorContains(t, err, "boom")
}
