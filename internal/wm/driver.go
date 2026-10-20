package wm

import "context"

type Driver interface {
	ListWindows(ctx context.Context, bundleID string) ([]Window, error)
	MoveToWorkspace(ctx context.Context, windowID, workspace string) error
	FocusWindow(ctx context.Context, windowID string) error
	FocusedWindow(ctx context.Context) (Window, bool, error)
	FocusedWorkspace(ctx context.Context) (string, error)
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

func (None) FocusedWindow(ctx context.Context) (Window, bool, error) {
	return Window{}, false, nil
}

func (None) FocusedWorkspace(ctx context.Context) (string, error) {
	return "", nil
}
