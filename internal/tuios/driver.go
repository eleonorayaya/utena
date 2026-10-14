package tuios

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"github.com/eleonorayaya/utena/internal/mux"
)

type tuiosEvent struct {
	Type    string `json:"type"`
	Session string `json:"session"`
	Window  string `json:"window"`
	State   string `json:"state"`
}

type agentPane struct {
	Session  string            `json:"session"`
	WindowID string            `json:"window_id"`
	State    string            `json:"state"`
	Meta     map[string]string `json:"meta"`
}

const agentSessionMetaKey = "session"

var followedEvents = []string{
	"session-created",
	"session-closed",
	"window-created",
	"window-closed",
	"window-retitled",
	"window-focused",
	"window-moved",
	"workspace-switched",
	"agent-state",
	"gap",
}

type tuiosError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *tuiosError) Error() string {
	return fmt.Sprintf("tuios %s: %s", e.Code, e.Message)
}

func isTuiosCode(err error, code string) bool {
	var te *tuiosError
	return errors.As(err, &te) && te.Code == code
}

type Driver struct {
	socketPath string
}

var _ mux.Driver = (*Driver)(nil)

func NewDriver() *Driver {
	return &Driver{socketPath: socketPath()}
}

func socketPath() string {
	if p := os.Getenv("TUIOS_SOCKET"); p != "" {
		return p
	}
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "tuios", "tuios.sock")
	}
	return filepath.Join("/tmp", fmt.Sprintf("tuios-%d", os.Getuid()), "tuios.sock")
}

func (d *Driver) dial() (net.Conn, error) {
	conn, err := net.DialTimeout("unix", d.socketPath, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("tuios daemon not running at %s (start it with `utena attach`): %w", d.socketPath, err)
	}
	return conn, nil
}

func (d *Driver) call(verb string, params map[string]any, out any) error {
	conn, err := d.dial()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	if params == nil {
		params = map[string]any{}
	}
	if err := json.NewEncoder(conn).Encode(map[string]any{"id": 1, "verb": verb, "params": params}); err != nil {
		return err
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *tuiosError     `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("tuios %s: reading response: %w", verb, err)
	}
	if resp.Error != nil {
		return resp.Error
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(resp.Result, out)
}

func loginShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/zsh"
}

func envArgv(env map[string]string, argv ...string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []string{"env"}
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return append(out, argv...)
}

func (d *Driver) NewSession(name, startDir string, env map[string]string) error {
	err := d.call("new-session", map[string]any{
		"name":    name,
		"cwd":     startDir,
		"command": envArgv(env, loginShell(), "-l"),
	}, nil)
	if isTuiosCode(err, "session_exists") {
		return nil
	}
	return err
}

func (d *Driver) NewWindow(session, startDir, command string, env map[string]string) error {
	return d.call("new-window", map[string]any{
		"session": session,
		"cwd":     startDir,
		"command": envArgv(env, loginShell(), "-lc", command),
	}, nil)
}

func (d *Driver) KillSession(name string) error {
	err := d.call("kill-session", map[string]any{"session": name}, nil)
	if isTuiosCode(err, "session_not_found") {
		return nil
	}
	return err
}

func (d *Driver) HasSession(name string) bool {
	names, err := d.ListSessions()
	if err != nil {
		slog.Warn("tuios list-sessions failed", "error", err)
		return false
	}
	return slices.Contains(names, name)
}

func (d *Driver) ListSessions() ([]string, error) {
	var res struct {
		Sessions []struct {
			Name string `json:"name"`
		} `json:"sessions"`
	}
	if err := d.call("list-sessions", nil, &res); err != nil {
		return nil, err
	}
	names := make([]string, len(res.Sessions))
	for i, s := range res.Sessions {
		names[i] = s.Name
	}
	return names, nil
}

func (d *Driver) ListWindows(session string) ([]mux.Window, error) {
	var res struct {
		Windows []struct {
			Index       int    `json:"index"`
			DisplayName string `json:"display_name"`
			Focused     bool   `json:"focused"`
		} `json:"windows"`
	}
	if err := d.call("list-windows", map[string]any{"session": session}, &res); err != nil {
		return nil, err
	}
	windows := make([]mux.Window, len(res.Windows))
	for i, w := range res.Windows {
		windows[i] = mux.Window{Index: w.Index, Name: w.DisplayName, Active: w.Focused}
	}
	return windows, nil
}

func (d *Driver) listAgents() ([]agentPane, error) {
	var res struct {
		Agents []agentPane `json:"agents"`
	}
	if err := d.call("list-agents", map[string]any{"all_sessions": true}, &res); err != nil {
		return nil, err
	}
	return res.Agents, nil
}

func (d *Driver) setAgentMeta(sessionName, windowID string, meta map[string]string) error {
	return d.call("set-agent-meta", map[string]any{
		"session": sessionName,
		"window":  windowID,
		"source":  "utena",
		"tokens":  meta,
	}, nil)
}

func (d *Driver) Subscribe(ctx context.Context, onEvent func(mux.Event)) error {
	conn, err := d.dial()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	if err := json.NewEncoder(conn).Encode(map[string]any{
		"id": 1, "verb": "subscribe", "params": map[string]any{"types": followedEvents},
	}); err != nil {
		return err
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !scanner.Scan() {
		return fmt.Errorf("tuios subscribe: no ack: %w", scanner.Err())
	}
	var ack struct {
		Error *tuiosError `json:"error"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &ack); err != nil {
		return fmt.Errorf("tuios subscribe: bad ack: %w", err)
	}
	if ack.Error != nil {
		return ack.Error
	}
	d.tagAgentPanes()
	onEvent(mux.Event{Type: mux.EventResync})

	for scanner.Scan() {
		var ev tuiosEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			slog.Warn("tuios: unparseable event", "error", err)
			continue
		}
		d.translate(ev, onEvent)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return errors.New("tuios event stream closed")
}

func (d *Driver) translate(ev tuiosEvent, onEvent func(mux.Event)) {
	switch ev.Type {
	case "gap":
		d.tagAgentPanes()
		onEvent(mux.Event{Type: mux.EventResync})
	case "session-created":
		onEvent(mux.Event{Type: mux.EventSessionCreated, Session: ev.Session})
	case "session-closed":
		onEvent(mux.Event{Type: mux.EventSessionClosed, Session: ev.Session})
	case "agent-state":
		d.tagAgentPane(ev.Session, ev.Window, ev.State)
	default:
		onEvent(mux.Event{Type: mux.EventWindowsChanged, Session: ev.Session})
	}
}

func (d *Driver) tagAgentPanes() {
	agents, err := d.listAgents()
	if err != nil {
		slog.Debug("tuios list-agents failed", "error", err)
		return
	}
	for _, a := range agents {
		if a.Meta[agentSessionMetaKey] == a.Session {
			continue
		}
		d.tagAgentPane(a.Session, a.WindowID, a.State)
	}
}

func (d *Driver) tagAgentPane(sessionName, windowID, state string) {
	if sessionName == "" || windowID == "" || state == "" || state == "none" {
		return
	}
	if err := d.setAgentMeta(sessionName, windowID, map[string]string{agentSessionMetaKey: sessionName}); err != nil {
		slog.Debug("tuios set-agent-meta failed", "session", sessionName, "window", windowID, "error", err)
	}
}

func CloseWindow(session, window string) error {
	if session == "" || window == "" {
		return errors.New("close-window needs an explicit session and window")
	}
	d := NewDriver()
	err := d.call("close-window", map[string]any{"session": session, "window": window}, nil)
	if isTuiosCode(err, "window_not_found") || isTuiosCode(err, "session_not_found") {
		return nil
	}
	return err
}
