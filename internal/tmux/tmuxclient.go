package tmux

import (
	"fmt"
	"log/slog"

	"github.com/GianlucaP106/gotmux/gotmux"
)

type tmuxRunner interface {
	newSession(name, startDir string, env map[string]string) error
	killSession(name string) error
	hasSession(name string) bool
	switchClient(targetSession string) error
	listSessionNames() ([]string, error)
	command(args ...string) (string, error)
}

type gotmuxRunner struct {
	tmux *gotmux.Tmux
}

func newGotmuxRunner() tmuxRunner {
	t, err := gotmux.DefaultTmux()
	if err != nil {
		slog.Warn("failed to initialize gotmux", "error", err)
		return nil
	}
	return &gotmuxRunner{tmux: t}
}

func (r *gotmuxRunner) newSession(name, startDir string, env map[string]string) error {
	_, err := r.tmux.NewSession(&gotmux.SessionOptions{
		Name:           name,
		StartDirectory: startDir,
	})
	if err != nil {
		return err
	}
	for k, v := range env {
		if _, err := r.tmux.Command("set-environment", "-t", name, k, v); err != nil {
			return fmt.Errorf("failed to set environment %s: %w", k, err)
		}
	}
	return nil
}

func (r *gotmuxRunner) killSession(name string) error {
	if !r.tmux.HasSession(name) {
		return nil
	}
	sess, err := r.tmux.GetSessionByName(name)
	if err != nil {
		return err
	}
	if sess == nil {
		return nil
	}
	return sess.Kill()
}

func (r *gotmuxRunner) hasSession(name string) bool {
	return r.tmux.HasSession(name)
}

func (r *gotmuxRunner) switchClient(targetSession string) error {
	return r.tmux.SwitchClient(&gotmux.SwitchClientOptions{
		TargetSession: targetSession,
	})
}

func (r *gotmuxRunner) listSessionNames() ([]string, error) {
	sessions, err := r.tmux.ListSessions()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(sessions))
	for i, s := range sessions {
		names[i] = s.Name
	}
	return names, nil
}

func (r *gotmuxRunner) command(args ...string) (string, error) {
	return r.tmux.Command(args...)
}
