package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type pickRow struct {
	key     string
	display string
}

func fzfPath() string {
	for _, c := range []string{"fzf", "/opt/homebrew/bin/fzf", "/usr/local/bin/fzf"} {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func buildPickRows(sessions []Session, ungrouped []liveWorkspace) []pickRow {
	var rows []pickRow
	for _, s := range sessions {
		if s.Archived {
			continue
		}
		mark := " "
		if s.Active() {
			mark = "●"
		}
		rows = append(rows, pickRow{
			key:     "session\t" + s.Name,
			display: fmt.Sprintf("%s %-34s %d repos", mark, s.Name, len(s.Checkouts)),
		})
		for _, c := range s.Checkouts {
			rows = append(rows, pickRow{
				key:     "workspace\t" + c.WorkspaceID + "\t" + s.Name,
				display: fmt.Sprintf("    %-30s %s", c.Label, c.Branch),
			})
		}
	}
	for _, w := range ungrouped {
		rows = append(rows, pickRow{
			key:     "workspace\t" + w.ID + "\t",
			display: fmt.Sprintf("  %-34s %s", w.Label, "workspace"),
		})
	}
	return rows
}

func runPick() error {
	fzf := fzfPath()
	if fzf == "" {
		return fmt.Errorf("fzf not found on PATH")
	}
	h := newHerdrClient()
	sessions, ungrouped, err := loadSessions(h)
	if err != nil {
		return err
	}
	rows := buildPickRows(sessions, ungrouped)
	if len(rows) == 0 {
		return fmt.Errorf("nothing to pick")
	}

	var in strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&in, "%s\t%s\n", r.key, r.display)
	}

	cmd := exec.Command(fzf,
		"--no-input", "--no-multi",
		"--delimiter=\t", "--with-nth=4..",
		"--prompt=go to> ",
		"--header=j/k move · / search · enter open · q quit",
		"--bind=j:down,k:up", "--bind=g:first,G:last",
		"--bind=/:show-input", "--bind=esc:hide-input+clear-query", "--bind=q:abort",
	)
	cmd.Stdin = strings.NewReader(in.String())
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && (ee.ExitCode() == 130 || ee.ExitCode() == 1) {
			return nil
		}
		return fmt.Errorf("fzf: %w", err)
	}
	if strings.TrimSpace(string(out)) == "" {
		return nil
	}

	parts := strings.Split(strings.TrimSpace(string(out)), "\t")
	switch parts[0] {
	case "session":
		name := parts[1]
		for _, s := range sessions {
			if s.Name != name {
				continue
			}
			if !s.Active() {
				if err := activateSession(h, name); err != nil {
					return err
				}
				sessions, _, _ = loadSessions(h)
				for _, r := range sessions {
					if r.Name == name && r.WorkspaceID != "" {
						return h.focusWorkspace(r.WorkspaceID)
					}
				}
				return nil
			}
			return h.focusWorkspace(s.WorkspaceID)
		}
	case "workspace":
		id := parts[1]
		if id != "" {
			return h.focusWorkspace(id)
		}
		if len(parts) > 2 && parts[2] != "" {
			return activateSession(h, parts[2])
		}
	}
	return nil
}
