package tmux

import (
	"fmt"
	"log/slog"

	"github.com/GianlucaP106/gotmux/gotmux"
)

type TmuxClient interface {
	CreateSession(name, startDir string, env map[string]string) error
	KillSession(name string) error
	HasSession(name string) bool
	ListSessionNames() ([]string, error)
	SwitchClient(targetSession string) error
	RunCommand(cmd ...string) (string, error)
}

type gotmuxClient struct {
	tmux *gotmux.Tmux
}

func NewGotmuxClient() TmuxClient {
	t, err := gotmux.DefaultTmux()
	if err != nil {
		slog.Warn("failed to initialize gotmux", "error", err)
		return nil
	}
	return &gotmuxClient{tmux: t}
}

func (c *gotmuxClient) CreateSession(name, startDir string, env map[string]string) error {
	_, err := c.tmux.NewSession(&gotmux.SessionOptions{
		Name:           name,
		StartDirectory: startDir,
	})
	if err != nil {
		return err
	}
	for k, v := range env {
		if _, err := c.tmux.Command("set-environment", "-t", name, k, v); err != nil {
			return fmt.Errorf("failed to set environment %s: %w", k, err)
		}
	}
	return nil
}

func (c *gotmuxClient) KillSession(name string) error {
	sess, err := c.tmux.GetSessionByName(name)
	if err != nil {
		return err
	}
	return sess.Kill()
}

func (c *gotmuxClient) HasSession(name string) bool {
	return c.tmux.HasSession(name)
}

func (c *gotmuxClient) ListSessionNames() ([]string, error) {
	sessions, err := c.tmux.ListSessions()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(sessions))
	for i, s := range sessions {
		names[i] = s.Name
	}
	return names, nil
}

func (c *gotmuxClient) SwitchClient(targetSession string) error {
	return c.tmux.SwitchClient(&gotmux.SwitchClientOptions{
		TargetSession: targetSession,
	})
}

func (c *gotmuxClient) RunCommand(cmd ...string) (string, error) {
	return c.tmux.Command(cmd...)
}
