package tmux

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
)

type tmuxRunner interface {
	newSession(name, startDir string, env map[string]string) error
	newWindow(sessionName, startDir, command string, env map[string]string) error
	killSession(name string) error
	hasSession(name string) bool
	listSessionNames() ([]string, error)
	listWindows(sessionName string) ([]Window, error)
	subscribe(ctx context.Context, types []string, onEvent func(tuiosEvent)) error
}

type tuiosEvent struct {
	Type    string `json:"type"`
	Session string `json:"session"`
	Reason  string `json:"reason"`
}

const tuiosEventSubscribed = "subscribed"

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

type tuiosRunner struct {
	socketPath string
}

func newTuiosRunner() tmuxRunner {
	return &tuiosRunner{socketPath: tuiosSocketPath()}
}

func tuiosSocketPath() string {
	if p := os.Getenv("TUIOS_SOCKET"); p != "" {
		return p
	}
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "tuios", "tuios.sock")
	}
	return filepath.Join("/tmp", fmt.Sprintf("tuios-%d", os.Getuid()), "tuios.sock")
}

func (r *tuiosRunner) dial() (net.Conn, error) {
	conn, err := net.DialTimeout("unix", r.socketPath, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("tuios daemon not running at %s (start it with `utena attach`): %w", r.socketPath, err)
	}
	return conn, nil
}

func (r *tuiosRunner) call(verb string, params map[string]any, out any) error {
	conn, err := r.dial()
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

func (r *tuiosRunner) newSession(name, startDir string, env map[string]string) error {
	err := r.call("new-session", map[string]any{
		"name":    name,
		"cwd":     startDir,
		"command": envArgv(env, loginShell(), "-l"),
	}, nil)
	if isTuiosCode(err, "session_exists") {
		return nil
	}
	return err
}

func (r *tuiosRunner) newWindow(sessionName, startDir, command string, env map[string]string) error {
	return r.call("new-window", map[string]any{
		"session": sessionName,
		"cwd":     startDir,
		"command": envArgv(env, loginShell(), "-lc", command),
	}, nil)
}

func (r *tuiosRunner) killSession(name string) error {
	err := r.call("kill-session", map[string]any{"session": name}, nil)
	if isTuiosCode(err, "session_not_found") {
		return nil
	}
	return err
}

func (r *tuiosRunner) hasSession(name string) bool {
	names, err := r.listSessionNames()
	if err != nil {
		slog.Warn("tuios list-sessions failed", "error", err)
		return false
	}
	return slices.Contains(names, name)
}

func (r *tuiosRunner) listSessionNames() ([]string, error) {
	var res struct {
		Sessions []struct {
			Name string `json:"name"`
		} `json:"sessions"`
	}
	if err := r.call("list-sessions", nil, &res); err != nil {
		return nil, err
	}
	names := make([]string, len(res.Sessions))
	for i, s := range res.Sessions {
		names[i] = s.Name
	}
	return names, nil
}

func (r *tuiosRunner) listWindows(sessionName string) ([]Window, error) {
	var res struct {
		Windows []struct {
			Index       int    `json:"index"`
			DisplayName string `json:"display_name"`
			Focused     bool   `json:"focused"`
		} `json:"windows"`
	}
	if err := r.call("list-windows", map[string]any{"session": sessionName}, &res); err != nil {
		return nil, err
	}
	windows := make([]Window, len(res.Windows))
	for i, w := range res.Windows {
		windows[i] = Window{Index: w.Index, Name: w.DisplayName, Active: w.Focused}
	}
	return windows, nil
}

func (r *tuiosRunner) subscribe(ctx context.Context, types []string, onEvent func(tuiosEvent)) error {
	conn, err := r.dial()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	if err := json.NewEncoder(conn).Encode(map[string]any{
		"id": 1, "verb": "subscribe", "params": map[string]any{"types": types},
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
	onEvent(tuiosEvent{Type: tuiosEventSubscribed})

	for scanner.Scan() {
		var ev tuiosEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			slog.Warn("tuios: unparseable event", "error", err)
			continue
		}
		onEvent(ev)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return errors.New("tuios event stream closed")
}

func CloseWindow(session, window string) error {
	if session == "" || window == "" {
		return errors.New("close-window needs an explicit session and window")
	}
	r := &tuiosRunner{socketPath: tuiosSocketPath()}
	err := r.call("close-window", map[string]any{"session": session, "window": window}, nil)
	if isTuiosCode(err, "window_not_found") || isTuiosCode(err, "session_not_found") {
		return nil
	}
	return err
}
