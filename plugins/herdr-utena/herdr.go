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

func (h *herdrClient) socketCall(method string, params map[string]any) error {
	path, err := socketPath()
	if err != nil {
		return err
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		return fmt.Errorf("connect to herdr socket: %w", err)
	}
	defer conn.Close()

	req, err := json.Marshal(map[string]any{"id": "herdr-utena", "method": method, "params": params})
	if err != nil {
		return fmt.Errorf("marshal %s: %w", method, err)
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return fmt.Errorf("send %s: %w", method, err)
	}

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("read %s response: %w", method, err)
	}
	var resp struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return fmt.Errorf("parse %s response: %w", method, err)
	}
	if resp.Error != nil {
		return fmt.Errorf("%s: %s: %s", method, resp.Error.Code, resp.Error.Message)
	}
	return nil
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
