package wm

import "context"

type Driver interface {
	ListWindows(ctx context.Context, bundleID string) ([]Window, error)
	MoveToWorkspace(ctx context.Context, windowID, workspace string) error
	FocusWindow(ctx context.Context, windowID string) error
}

type Window struct {
	ID        string
	Title     string
	Workspace string
}

type None struct{}

var _ Driver = None{}

func (None) ListWindows(ctx context.Context, bundleID string) ([]Window, error) {
	return nil, nil
}

func (None) MoveToWorkspace(ctx context.Context, windowID, workspace string) error {
	return nil
}

func (None) FocusWindow(ctx context.Context, windowID string) error {
	return nil
}
