package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type herdrClient struct {
	bin string
}

func newHerdrClient() *herdrClient {
	bin := os.Getenv("HERDR_BIN_PATH")
	if bin == "" {
		bin = "herdr"
	}
	return &herdrClient{bin: bin}
}

type workspaceRef struct {
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
}

type herdrEnvelope struct {
	Result struct {
		Type        string       `json:"type"`
		AlreadyOpen bool         `json:"already_open"`
		Workspace   workspaceRef `json:"workspace"`
		Workspaces  []struct {
			workspaceRef
			Focused bool `json:"focused"`
		} `json:"workspaces"`
	} `json:"result"`
}

func (h *herdrClient) run(args ...string) (*herdrEnvelope, error) {
	cmd := exec.Command(h.bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("herdr %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	var env herdrEnvelope
	if err := json.Unmarshal(out, &env); err != nil {
		return nil, fmt.Errorf("herdr %s: parse response: %w", strings.Join(args, " "), err)
	}
	return &env, nil
}

func (h *herdrClient) createWorkspace(cwd, label string) (string, error) {
	env, err := h.run("workspace", "create", "--cwd", cwd, "--label", label, "--no-focus")
	if err != nil {
		return "", err
	}
	return env.Result.Workspace.WorkspaceID, nil
}

func socketPath() (string, error) {
	if p := os.Getenv("HERDR_SOCKET_PATH"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "herdr", "herdr.sock"), nil
}

func (h *herdrClient) socketRequest(method string, params map[string]any) (json.RawMessage, error) {
	path, err := socketPath()
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("connect to herdr socket: %w", err)
	}
	defer conn.Close()

	req, err := json.Marshal(map[string]any{"id": "herdr-utena", "method": method, "params": params})
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", method, err)
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return nil, fmt.Errorf("send %s: %w", method, err)
	}

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read %s response: %w", method, err)
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("parse %s response: %w", method, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("%s: %s: %s", method, resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

func (h *herdrClient) socketCall(method string, params map[string]any) error {
	_, err := h.socketRequest(method, params)
	return err
}

type wsInfo struct {
	ID          string `json:"workspace_id"`
	Label       string `json:"label"`
	AgentStatus string `json:"agent_status"`
	Focused     bool   `json:"focused"`
	PaneCount   int    `json:"pane_count"`
}

func (h *herdrClient) listWorkspaces() (map[string]wsInfo, error) {
	raw, err := h.socketRequest("workspace.list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Workspaces []wsInfo `json:"workspaces"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse workspace list: %w", err)
	}
	byID := make(map[string]wsInfo, len(out.Workspaces))
	for _, w := range out.Workspaces {
		byID[w.ID] = w
	}
	return byID, nil
}

type snapshotPane struct {
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	Cwd         string `json:"cwd"`
	Focused     bool   `json:"focused"`
}

type snapshotLayout struct {
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	Panes       []struct {
		PaneID string `json:"pane_id"`
		Rect   struct {
			X int `json:"x"`
			Y int `json:"y"`
		} `json:"rect"`
	} `json:"panes"`
}

type snapshot struct {
	FocusedWorkspaceID string           `json:"focused_workspace_id"`
	FocusedTabID       string           `json:"focused_tab_id"`
	Panes              []snapshotPane   `json:"panes"`
	Layouts            []snapshotLayout `json:"layouts"`
}

func (h *herdrClient) snapshot() (*snapshot, error) {
	raw, err := h.socketRequest("session.snapshot", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Snapshot snapshot `json:"snapshot"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse snapshot: %w", err)
	}
	return &out.Snapshot, nil
}

func (s *snapshot) leftmostPane(tabID string) string {
	best, bestX := "", 1<<30
	for _, l := range s.Layouts {
		if l.TabID != tabID {
			continue
		}
		for _, p := range l.Panes {
			if p.Rect.X < bestX {
				best, bestX = p.PaneID, p.Rect.X
			}
		}
	}
	return best
}

func (h *herdrClient) focusPane(id string) error {
	return h.socketCall("pane.focus", map[string]any{"pane_id": id})
}

func (h *herdrClient) swapPanes(source, target string) error {
	return h.socketCall("pane.swap", map[string]any{
		"source_pane_id": source,
		"target_pane_id": target,
	})
}

func (h *herdrClient) openPluginPane(pluginID, entrypoint, targetPane, direction string) (string, error) {
	params := map[string]any{
		"plugin_id":  pluginID,
		"entrypoint": entrypoint,
		"placement":  "split",
		"focus":      true,
	}
	if targetPane != "" {
		params["target_pane_id"] = targetPane
	}
	if direction != "" {
		params["direction"] = direction
	}
	raw, err := h.socketRequest("plugin.pane.open", params)
	if err != nil {
		return "", err
	}
	var out struct {
		PluginPane struct {
			Pane struct {
				PaneID string `json:"pane_id"`
			} `json:"pane"`
		} `json:"plugin_pane"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("parse plugin pane open: %w", err)
	}
	return out.PluginPane.Pane.PaneID, nil
}

func (h *herdrClient) subscribe(types []string) (<-chan string, error) {
	path, err := socketPath()
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("connect to herdr socket: %w", err)
	}

	subs := make([]map[string]string, 0, len(types))
	for _, t := range types {
		subs = append(subs, map[string]string{"type": t})
	}
	req, err := json.Marshal(map[string]any{
		"id":     "herdr-utena-events",
		"method": "events.subscribe",
		"params": map[string]any{"subscriptions": subs},
	})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("marshal subscribe: %w", err)
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send subscribe: %w", err)
	}

	out := make(chan string, 16)
	go func() {
		defer conn.Close()
		defer close(out)
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadBytes('\n')
			if err != nil {
				return
			}
			var ev struct {
				Event string `json:"event"`
			}
			if json.Unmarshal(line, &ev) != nil || ev.Event == "" {
				continue
			}
			select {
			case out <- ev.Event:
			default:
			}
		}
	}()
	return out, nil
}

func (h *herdrClient) focusWorkspace(id string) error {
	return h.socketCall("workspace.focus", map[string]any{"workspace_id": id})
}

func (h *herdrClient) closeWorkspace(id string) error {
	return h.socketCall("workspace.close", map[string]any{"workspace_id": id})
}

func (h *herdrClient) tagSession(workspaceID, session string) error {
	return h.socketCall("workspace.report_metadata", map[string]any{
		"workspace_id": workspaceID,
		"source":       "herdr-utena",
		"tokens":       map[string]string{"session": session},
	})
}

func (h *herdrClient) groupWorkspaces(ids []string) error {
	return h.socketCall("workspace.move_block", map[string]any{"workspace_ids": ids})
}

func (h *herdrClient) workspaceExists(id string) bool {
	env, err := h.run("workspace", "list")
	if err != nil {
		return false
	}
	for _, w := range env.Result.Workspaces {
		if w.WorkspaceID == id {
			return true
		}
	}
	return false
}
