package tmux

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

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
}

func startFakeTuios(t *testing.T, replies map[string]string) (*tuiosRunner, *fakeTuiosDaemon) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tuios-test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	sock := filepath.Join(dir, "tuios.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	d := &fakeTuiosDaemon{replies: replies}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go d.serve(conn)
		}
	}()
	return &tuiosRunner{socketPath: sock}, d
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
}

func (d *fakeTuiosDaemon) last() fakeTuiosRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.requests[len(d.requests)-1]
}

func TestTuiosRunner_NewSessionInjectsEnvAndToleratesExisting(t *testing.T) {
	r, d := startFakeTuios(t, map[string]string{
		"new-session": `{"id":1,"error":{"code":"session_exists","message":"exists"}}`,
	})
	t.Setenv("SHELL", "/bin/zsh")

	require.NoError(t, r.newSession("work", "/src", map[string]string{"UTENA_SESSION_ID": "7"}))

	req := d.last()
	assert.Equal(t, "work", req.Params["name"])
	assert.Equal(t, "/src", req.Params["cwd"])
	assert.Equal(t, []any{"env", "UTENA_SESSION_ID=7", "/bin/zsh", "-l"}, req.Params["command"])
}

func TestTuiosRunner_NewWindowRunsCommandThroughLoginShell(t *testing.T) {
	r, d := startFakeTuios(t, map[string]string{
		"new-window": `{"id":1,"result":{"type":"window_created"}}`,
	})
	t.Setenv("SHELL", "/bin/zsh")

	require.NoError(t, r.newWindow("work", "/src", `claude "hi"`, map[string]string{"UTENA_SESSION_ID": "7"}))

	assert.Equal(t, []any{"env", "UTENA_SESSION_ID=7", "/bin/zsh", "-lc", `claude "hi"`}, d.last().Params["command"])
}

func TestTuiosRunner_KillMissingSessionIsNotAnError(t *testing.T) {
	r, _ := startFakeTuios(t, map[string]string{
		"kill-session": `{"id":1,"error":{"code":"session_not_found","message":"nope"}}`,
	})
	require.NoError(t, r.killSession("gone"))
}

func TestTuiosRunner_OtherErrorsSurface(t *testing.T) {
	r, _ := startFakeTuios(t, map[string]string{
		"kill-session": `{"id":1,"error":{"code":"internal","message":"boom"}}`,
	})
	err := r.killSession("x")
	require.Error(t, err)
	assert.True(t, isTuiosCode(err, "internal"))
}

func TestTuiosRunner_ListWindowsAndHasSession(t *testing.T) {
	r, _ := startFakeTuios(t, map[string]string{
		"list-windows":  `{"id":1,"result":{"windows":[{"index":0,"display_name":"zsh","focused":false},{"index":1,"display_name":"claude","focused":true}]}}`,
		"list-sessions": `{"id":1,"result":{"sessions":[{"name":"work"}]}}`,
	})

	windows, err := r.listWindows("work")
	require.NoError(t, err)
	assert.Equal(t, []Window{{Index: 0, Name: "zsh"}, {Index: 1, Name: "claude", Active: true}}, windows)

	assert.True(t, r.hasSession("work"))
	assert.False(t, r.hasSession("other"))
}

func TestTuiosRunner_DaemonDownIsAnErrorNotAStart(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "tuios-test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "tuios.sock")
	r := &tuiosRunner{socketPath: sock}

	err = r.newSession("work", "/src", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "utena attach")
	assert.NoFileExists(t, sock)
}
