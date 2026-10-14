package tuios

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

func currentWorkspace(workspaces []workspaceInfo) int {
	for _, w := range workspaces {
		if w.Current {
			return w.Workspace
		}
	}
	return 0
}

func pickEmptyWorkspace(workspaces []workspaceInfo) (int, bool) {
	current := currentWorkspace(workspaces)
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

func listWorkspaces(d *Driver, session string) ([]workspaceInfo, error) {
	var res struct {
		Workspaces []workspaceInfo `json:"workspaces"`
	}
	if err := d.call("list-workspaces", map[string]any{"session": session}, &res); err != nil {
		return nil, err
	}
	return res.Workspaces, nil
}

func SwitchWorkspace(session string, step int) error {
	if session == "" {
		return errors.New("no tuios session: run from inside tuios")
	}
	d := NewDriver()
	workspaces, err := listWorkspaces(d, session)
	if err != nil {
		return err
	}
	target := stepWorkspace(workspaces, step)
	return d.call("select-workspace", map[string]any{"session": session, "workspace": target}, nil)
}

func NewWorkspaceWindow(session, cwd string) error {
	if session == "" {
		return errors.New("no tuios session: run from inside tuios")
	}
	d := NewDriver()
	workspaces, err := listWorkspaces(d, session)
	if err != nil {
		return err
	}
	target, ok := pickEmptyWorkspace(workspaces)
	if !ok {
		return ErrNoEmptyWorkspace
	}
	if err := d.call("select-workspace", map[string]any{"session": session, "workspace": target}, nil); err != nil {
		return err
	}
	params := map[string]any{"session": session, "workspace": target, "focus": true}
	if cwd != "" {
		params["cwd"] = cwd
	}
	if err := d.call("new-window", params, nil); err != nil {
		if previous := currentWorkspace(workspaces); previous != 0 {
			_ = d.call("select-workspace", map[string]any{"session": session, "workspace": previous}, nil)
		}
		return err
	}
	return nil
}
