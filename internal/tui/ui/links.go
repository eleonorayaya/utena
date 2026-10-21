package ui

import (
	"os/exec"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/git"
)

func OpenablePRs(prs []git.PullRequest) []git.PullRequest {
	var out []git.PullRequest
	for _, p := range prs {
		if p.HTMLURL == "" {
			continue
		}
		if p.State == git.PRStateOpen || p.State == git.PRStateDraft {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].State != out[j].State {
			return out[i].State == git.PRStateOpen
		}
		return out[i].Number > out[j].Number
	})
	return out
}

type OpenURLFailedMsg struct{ Err error }

func OpenURL(url string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("open", url)
		if err := cmd.Start(); err != nil {
			return OpenURLFailedMsg{Err: err}
		}
		go cmd.Wait()
		return nil
	}
}
