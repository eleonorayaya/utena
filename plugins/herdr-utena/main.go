package main

import (
	"flag"
	"fmt"
	"os"
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
		fmt.Fprintf(os.Stderr, "herdr-utena: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `herdr-utena — multi-repo sessions for herdr

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

const pluginID = "eleonorayaya.herdr-utena"

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
		if p.WorkspaceID == snap.FocusedWorkspaceID && sameDir(p.Cwd, root) {
			return h.focusPane(p.PaneID)
		}
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

func runSidebar() error {
	if path, err := utenaThemePath(); err == nil {
		_ = theme.Load(path)
	}
	_, err := tea.NewProgram(newSidebar(newHerdrClient()), tea.WithAltScreen()).Run()
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
		return fmt.Errorf("usage: herdr-utena %s <session-name>", verb)
	}
	if err := fn(newHerdrClient(), args[0]); err != nil {
		return err
	}
	fmt.Printf("%sd %s\n", verb, args[0])
	return nil
}

func runList() error {
	state, err := loadState()
	if err != nil {
		return err
	}
	if len(state.Sessions) == 0 {
		fmt.Println("no sessions")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATUS\tREPOS\tROOT")
	for _, s := range state.Sessions {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", s.Name, s.Status, len(s.Checkouts), s.Root)
	}
	return w.Flush()
}
