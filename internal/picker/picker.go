package picker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/eleonorayaya/utena/internal/wm"
)

const (
	EnvPicker  = "UTENA_PICKER"
	EnvPark    = "UTENA_PICKER_PARK"
	EnvTitle   = "UTENA_PICKER_TITLE"
	envCommand = "/usr/bin/env"
)

type Launcher interface {
	Launch(argv []string) error
}

type ExecLauncher struct{}

func (ExecLauncher) Launch(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", argv[0], err)
	}
	return cmd.Process.Release()
}

func DefaultReturnPath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "utena", "picker-return-window")
}

type Opener struct {
	WM            wm.Driver
	Launcher      Launcher
	AppID         string
	Title         string
	Utena         string
	ParkWorkspace string
	ReturnPath    string
}

func (o Opener) Open(ctx context.Context, terminal []string) error {
	if len(terminal) == 0 {
		return errors.New("picker: no terminal command")
	}
	if o.Title == "" {
		return errors.New("picker: empty window title")
	}

	focused, hasFocused, err := o.WM.FocusedWindow(ctx)
	if err != nil {
		slog.WarnContext(ctx, "picker: focused window lookup failed", "error", err)
		hasFocused = false
	}
	if hasFocused && focused.Title == o.Title {
		return nil
	}

	returnTo := ""
	if hasFocused {
		returnTo = focused.ID
	}
	if err := o.recordReturn(returnTo); err != nil {
		slog.WarnContext(ctx, "picker: record return window failed", "error", err)
	}

	if w, ok := o.findOpen(ctx); ok {
		return o.show(ctx, w, focused.Workspace)
	}
	return o.Launcher.Launch(LaunchArgv(terminal, o.Utena, o.Title, o.ParkWorkspace))
}

func (o Opener) show(ctx context.Context, w wm.Window, workspace string) error {
	if workspace == "" {
		ws, err := o.WM.FocusedWorkspace(ctx)
		if err != nil {
			slog.WarnContext(ctx, "picker: focused workspace lookup failed", "error", err)
		}
		workspace = ws
	}
	if workspace != "" && w.Workspace != workspace {
		if err := o.WM.MoveToWorkspace(ctx, w.ID, workspace); err != nil {
			return fmt.Errorf("picker: unpark picker: %w", err)
		}
	}
	if err := o.WM.FocusWindow(ctx, w.ID); err != nil {
		return fmt.Errorf("picker: focus open picker: %w", err)
	}
	return nil
}

func (o Opener) recordReturn(windowID string) error {
	if o.ReturnPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(o.ReturnPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(o.ReturnPath, []byte(windowID), 0o600)
}

func (o Opener) findOpen(ctx context.Context) (wm.Window, bool) {
	if o.AppID == "" {
		return wm.Window{}, false
	}
	windows, err := o.WM.ListWindows(ctx, o.AppID)
	if err != nil {
		slog.WarnContext(ctx, "picker: list windows failed", "error", err)
		return wm.Window{}, false
	}
	return MatchWindow(windows, o.Title)
}

func MatchWindow(windows []wm.Window, title string) (wm.Window, bool) {
	for _, w := range windows {
		if w.Title == title {
			return w, true
		}
	}
	return wm.Window{}, false
}

func LaunchArgv(terminal []string, utena, title, parkWorkspace string) []string {
	argv := append([]string(nil), terminal...)
	argv = append(argv, envCommand, EnvPicker+"=1", EnvTitle+"="+title)
	if parkWorkspace != "" {
		argv = append(argv, EnvPark+"="+parkWorkspace)
	}
	return append(argv, utena)
}

func Park(ctx context.Context, d wm.Driver, title, workspace, returnPath string) error {
	w, ok, err := d.FocusedWindow(ctx)
	if err != nil {
		return fmt.Errorf("picker: park: %w", err)
	}
	if ok && w.Title == title && w.Workspace != workspace {
		if err := d.MoveToWorkspace(ctx, w.ID, workspace); err != nil {
			return fmt.Errorf("picker: park: %w", err)
		}
	}
	return ReturnFocus(ctx, d, returnPath)
}

func ReturnFocus(ctx context.Context, d wm.Driver, returnPath string) error {
	data, err := os.ReadFile(returnPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("picker: read return window: %w", err)
	}
	id := strings.TrimSpace(string(data))
	if id == "" {
		return nil
	}
	if err := d.FocusWindow(ctx, id); err != nil {
		return fmt.Errorf("picker: return focus: %w", err)
	}
	return nil
}
