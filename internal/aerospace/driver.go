package aerospace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"

	"github.com/eleonorayaya/utena/internal/wm"
)

type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return out, fmt.Errorf("%w: %s", err, bytes.TrimSpace(exitErr.Stderr))
	}
	return out, err
}

type Driver struct {
	runner Runner
}

var _ wm.Driver = (*Driver)(nil)

func NewDriver() *Driver {
	return newDriver(execRunner{})
}

func newDriver(r Runner) *Driver {
	return &Driver{runner: r}
}

type wireWindow struct {
	ID        int    `json:"window-id"`
	Title     string `json:"window-title"`
	Workspace string `json:"workspace"`
}

func (d *Driver) ListWindows(ctx context.Context, bundleID string) ([]wm.Window, error) {
	if bundleID == "" {
		return nil, errors.New("aerospace: empty app bundle id")
	}
	out, err := d.runner.Run(ctx, "aerospace", "list-windows",
		"--monitor", "all",
		"--app-bundle-id", bundleID,
		"--json",
		"--format", "%{window-id} %{window-title} %{workspace}",
	)
	if err != nil {
		return nil, fmt.Errorf("aerospace list-windows: %w", err)
	}
	var wire []wireWindow
	if err := json.Unmarshal(out, &wire); err != nil {
		return nil, fmt.Errorf("aerospace list-windows: %w", err)
	}
	windows := make([]wm.Window, len(wire))
	for i, w := range wire {
		windows[i] = wm.Window{ID: strconv.Itoa(w.ID), Title: w.Title, Workspace: w.Workspace}
	}
	return windows, nil
}

func (d *Driver) MoveToWorkspace(ctx context.Context, windowID, workspace string) error {
	if _, err := d.runner.Run(ctx, "aerospace", "move-node-to-workspace", "--window-id", windowID, workspace); err != nil {
		return fmt.Errorf("aerospace move-node-to-workspace: %w", err)
	}
	return nil
}

func (d *Driver) FocusWindow(ctx context.Context, windowID string) error {
	if _, err := d.runner.Run(ctx, "aerospace", "focus", "--window-id", windowID); err != nil {
		return fmt.Errorf("aerospace focus: %w", err)
	}
	return nil
}
