package picker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"syscall"

	"github.com/eleonorayaya/utena/internal/wm"
)

const (
	EnvPicker       = "UTENA_PICKER"
	EnvReturnWindow = "UTENA_PICKER_RETURN_WINDOW"
	envCommand      = "/usr/bin/env"
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

type Opener struct {
	WM       wm.Driver
	Launcher Launcher
	AppID    string
	Title    string
	Utena    string
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

	if id, ok := o.findOpen(ctx); ok {
		if err := o.WM.FocusWindow(ctx, id); err != nil {
			return fmt.Errorf("picker: focus open picker: %w", err)
		}
		return nil
	}

	returnTo := ""
	if hasFocused {
		returnTo = focused.ID
	}
	return o.Launcher.Launch(LaunchArgv(terminal, o.Utena, returnTo))
}

func (o Opener) findOpen(ctx context.Context) (string, bool) {
	if o.AppID == "" {
		return "", false
	}
	windows, err := o.WM.ListWindows(ctx, o.AppID)
	if err != nil {
		slog.WarnContext(ctx, "picker: list windows failed", "error", err)
		return "", false
	}
	return MatchWindow(windows, o.Title)
}

func MatchWindow(windows []wm.Window, title string) (string, bool) {
	for _, w := range windows {
		if w.Title == title {
			return w.ID, true
		}
	}
	return "", false
}

func LaunchArgv(terminal []string, utena, returnTo string) []string {
	argv := append([]string(nil), terminal...)
	argv = append(argv, envCommand, "-u", EnvReturnWindow, EnvPicker+"=1")
	if returnTo != "" {
		argv = append(argv, EnvReturnWindow+"="+returnTo)
	}
	return append(argv, utena)
}

func ReturnFocus(ctx context.Context, d wm.Driver, getenv func(string) string) error {
	if getenv(EnvPicker) == "" {
		return nil
	}
	id := getenv(EnvReturnWindow)
	if id == "" {
		return nil
	}
	if err := d.FocusWindow(ctx, id); err != nil {
		return fmt.Errorf("picker: return focus: %w", err)
	}
	return nil
}
