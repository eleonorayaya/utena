package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/tabwriter"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eleonorayaya/utena/internal/tui/theme"
)

type repoList []string

func (r *repoList) String() string { return fmt.Sprint(*r) }

func (r *repoList) Set(v string) error {
	*r = append(*r, v)
	return nil
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "ping":
		err = runPing()
	case "doctor":
		err = runDoctor()
	case "pick":
		err = runPick()
	case "open-pick":
		err = runOpenPick()
	case "new":
		err = runNew(os.Args[2:])
	case "list":
		err = runList()
	case "sidebar":
		err = runSidebar()
	case "open-sidebar":
		err = runOpenSidebar()
	case "archive":
		err = runOneArg(os.Args[2:], "archive", archiveSession)
	case "delete":
		err = runOneArg(os.Args[2:], "delete", deleteSession)
	case "-h", "--help", "help":
		usage()
		return
	default:
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "utena: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `utena — multi-repo sessions for herdr

  ping                                     check the herdr connection
  new -branch B -repo P [-repo P] [-name N]  create a multi-repo session
  list                                     list sessions
`)
}

func runPing() error {
	h := newHerdrClient()
	env, err := h.run("workspace", "list")
	if err != nil {
		return err
	}
	fmt.Printf("ok: herdr reachable, %d workspace(s)\n", len(env.Result.Workspaces))
	return nil
}

func runNew(args []string) error {
	fs := flag.NewFlagSet("new", flag.ExitOnError)
	name := fs.String("name", "", "session name (defaults to the branch name)")
	branch := fs.String("branch", "", "branch to check out in every repo")
	var repos repoList
	fs.Var(&repos, "repo", "path to a git repository (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	sess, err := createSession(newHerdrClient(), createInput{
		Name:   *name,
		Branch: *branch,
		Repos:  repos,
	})
	if err != nil {
		return err
	}

	fmt.Printf("created session %q at %s\n", sess.Name, sess.Root)
	for _, c := range sess.Checkouts {
		fmt.Printf("  %-20s %s (%s)\n", c.Label, c.Branch, c.WorkspaceID)
	}
	fmt.Printf("  %-20s %s\n", "session workspace", sess.WorkspaceID)
	return nil
}

const pluginID = "eleonorayaya.utena"

func runOpenSidebar() error {
	h := newHerdrClient()
	snap, err := h.snapshot()
	if err != nil {
		return err
	}

	root, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve plugin root: %w", err)
	}
	root = filepath.Dir(filepath.Dir(root))

	for _, p := range snap.Panes {
		if p.WorkspaceID != snap.FocusedWorkspaceID || !sameDir(p.Cwd, root) {
			continue
		}
		if p.PaneID == snap.FocusedPaneID {
			return h.closePane(p.PaneID)
		}
		return h.focusPane(p.PaneID)
	}

	leftmost := snap.leftmostPane(snap.FocusedTabID)
	pane, err := h.openPluginPane(pluginID, "sidebar", leftmost, "right")
	if err != nil {
		return err
	}
	if leftmost != "" && pane != "" {
		if err := h.swapPanes(pane, leftmost); err != nil {
			return fmt.Errorf("move sidebar to the left: %w", err)
		}
	}
	return nil
}

func sameDir(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = a
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = b
	}
	return ra == rb
}

func runOpenPick() error {
	h := newHerdrClient()
	_ = h.socketCall("popup.close", map[string]any{})
	_, err := h.socketRequest("plugin.pane.open", map[string]any{
		"plugin_id": pluginID, "entrypoint": "picker",
		"placement": "popup", "focus": true,
	})
	return err
}

func runSidebar() error { return runSessionUI(false) }

func runSessionUI(popup bool) error {
	if path, err := utenaThemePath(); err == nil {
		_ = theme.Load(path)
	}
	m := newSidebar(newHerdrClient())
	m.popup = popup
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func utenaThemePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "utena", "theme.json"), nil
}

func runOneArg(args []string, verb string, fn func(*herdrClient, string) error) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: utena %s <session-name>", verb)
	}
	if err := fn(newHerdrClient(), args[0]); err != nil {
		return err
	}
	fmt.Printf("%sd %s\n", verb, args[0])
	return nil
}

func runList() error {
	sessions, _, err := loadSessions(newHerdrClient())
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Println("no sessions")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATE\tREPOS\tROOT")
	for _, s := range sessions {
		state := "inactive"
		switch {
		case s.Broken:
			state = "broken"
		case s.Archived:
			state = "archived"
		case s.Active():
			state = "active"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", s.Name, state, len(s.Checkouts), s.Root)
	}
	return w.Flush()
}

func runDoctor() error {
	home, herr := os.UserHomeDir()
	fmt.Printf("HOME env        = %q\n", os.Getenv("HOME"))
	fmt.Printf("UserHomeDir()   = %q (err=%v)\n", home, herr)
	fmt.Printf("PATH            = %q\n", os.Getenv("PATH"))
	if _, err := exec.LookPath("git"); err != nil {
		fmt.Printf("git on PATH     = NOT FOUND (%v)\n", err)
	} else {
		fmt.Printf("git on PATH     = ok\n")
	}
	roots := sessionRoots()
	fmt.Printf("sessionRoots()  = %v\n", roots)
	for _, r := range roots {
		entries, err := os.ReadDir(r)
		if err != nil {
			fmt.Printf("  %s -> ERROR %v\n", r, err)
			continue
		}
		n := 0
		for _, e := range entries {
			if e.IsDir() && isSessionRoot(filepath.Join(r, e.Name())) {
				n++
			}
		}
		fmt.Printf("  %s -> %d entries, %d sessions\n", r, len(entries), n)
	}
	fmt.Printf("scanSessions()  = %d\n", len(scanSessions()))

	fmt.Printf("config          = %s\n", configPath())
	fmt.Printf("repoRoots()     = %v\n", repoRoots())
	repos := discoverRepos()
	fmt.Printf("discoverRepos() = %d\n", len(repos))
	for _, r := range repos {
		fmt.Printf("  %s\n", r)
	}
	return nil
}
