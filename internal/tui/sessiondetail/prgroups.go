package sessiondetail

import (
	"sort"

	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
)

type prGroup struct {
	name string
	prs  []git.PullRequest
}

var prStateRank = map[git.PRState]int{
	git.PRStateOpen:   0,
	git.PRStateDraft:  1,
	git.PRStateMerged: 2,
	git.PRStateClosed: 3,
}

func sortPRs(prs []git.PullRequest) {
	sort.SliceStable(prs, func(i, j int) bool {
		ri, rj := prStateRank[prs[i].State], prStateRank[prs[j].State]
		if ri != rj {
			return ri < rj
		}
		return prs[i].Number > prs[j].Number
	})
}

func prMatchesWorktree(pr git.PullRequest, wt *git.Worktree) bool {
	if wt == nil {
		return false
	}
	if pr.RepoID != 0 && wt.RepoID != 0 {
		return pr.RepoID == wt.RepoID
	}
	return pr.HeadBranchID != nil && *pr.HeadBranchID == wt.BranchID
}

func groupPRs(s session.Session) []prGroup {
	if len(s.Worktrees) == 0 {
		prs := append([]git.PullRequest(nil), s.PullRequests...)
		sortPRs(prs)
		return []prGroup{{name: s.WorkspaceDisplay(), prs: prs}}
	}

	wts := append([]session.SessionWorktree(nil), s.Worktrees...)
	sort.SliceStable(wts, func(i, j int) bool { return wts[i].Position < wts[j].Position })

	assigned := make([]bool, len(s.PullRequests))
	var groups []prGroup
	for _, sw := range wts {
		name := "workspace"
		if sw.Workspace != nil && sw.Workspace.Name != "" {
			name = sw.Workspace.Name
		}
		g := prGroup{name: name}
		for i, pr := range s.PullRequests {
			if !assigned[i] && prMatchesWorktree(pr, sw.Worktree) {
				g.prs = append(g.prs, pr)
				assigned[i] = true
			}
		}
		sortPRs(g.prs)
		groups = append(groups, g)
	}

	other := prGroup{name: "Other"}
	for i, pr := range s.PullRequests {
		if !assigned[i] {
			other.prs = append(other.prs, pr)
		}
	}
	if len(other.prs) > 0 {
		sortPRs(other.prs)
		groups = append(groups, other)
	}
	return groups
}
