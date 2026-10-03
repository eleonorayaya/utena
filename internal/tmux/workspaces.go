package tmux

import (
	"errors"
	"slices"
)

var ErrNoEmptyWorkspace = errors.New("all 9 workspaces are in use")

type workspaceInfo struct {
	Workspace   int  `json:"workspace"`
	WindowCount int  `json:"window_count"`
	Current     bool `json:"current"`
}

func stepWorkspace(workspaces []workspaceInfo, step int) int {
	var used []int
	current := 0
	for _, w := range workspaces {
		if w.Current {
			current = w.Workspace
		}
		if w.Current || w.WindowCount > 0 {
			used = append(used, w.Workspace)
		}
	}
	i := slices.Index(used, current)
	if i < 0 {
		return current
	}
	return used[((i+step)%len(used)+len(used))%len(used)]
}

func pickEmptyWorkspace(workspaces []workspaceInfo) (int, bool) {
	current := 0
	for _, w := range workspaces {
		if w.Current {
			current = w.Workspace
		}
	}
	first := 0
	for _, w := range workspaces {
		if w.WindowCount > 0 || w.Current {
			continue
		}
		if w.Workspace > current {
			return w.Workspace, true
		}
		if first == 0 {
			first = w.Workspace
		}
	}
	return first, first != 0
}

func listWorkspaces(r *tuiosRunner, session string) ([]workspaceInfo, error) {
	var res struct {
		Workspaces []workspaceInfo `json:"workspaces"`
	}
	if err := r.call("list-workspaces", map[string]any{"session": session}, &res); err != nil {
		return nil, err
	}
	return res.Workspaces, nil
}

func SwitchWorkspace(session string, step int) error {
	if session == "" {
		return errors.New("no tuios session: run from inside tuios")
	}
	r := &tuiosRunner{socketPath: tuiosSocketPath()}
	workspaces, err := listWorkspaces(r, session)
	if err != nil {
		return err
	}
	target := stepWorkspace(workspaces, step)
	return r.call("select-workspace", map[string]any{"session": session, "workspace": target}, nil)
}

func NewWorkspaceWindow(session, cwd string) error {
	if session == "" {
		return errors.New("no tuios session: run from inside tuios")
	}
	r := &tuiosRunner{socketPath: tuiosSocketPath()}
	workspaces, err := listWorkspaces(r, session)
	if err != nil {
		return err
	}
	target, ok := pickEmptyWorkspace(workspaces)
	if !ok {
		return ErrNoEmptyWorkspace
	}
	params := map[string]any{"session": session, "workspace": target, "focus": true}
	if cwd != "" {
		params["cwd"] = cwd
	}
	if err := r.call("new-window", params, nil); err != nil {
		return err
	}
	return r.call("select-workspace", map[string]any{"session": session, "workspace": target}, nil)
}
