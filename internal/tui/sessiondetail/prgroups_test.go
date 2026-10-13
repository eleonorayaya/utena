package sessiondetail

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func wt(pos int, name string, repoID, branchID uint) session.SessionWorktree {
	return session.SessionWorktree{
		Position:  pos,
		Worktree:  &git.Worktree{RepoID: repoID, BranchID: branchID},
		Workspace: &workspace.Workspace{Model: gorm.Model{ID: repoID * 10}, Name: name},
	}
}

func groupSummary(gs []prGroup) map[string][]int {
	out := map[string][]int{}
	for _, g := range gs {
		nums := []int{}
		for _, p := range g.prs {
			nums = append(nums, p.Number)
		}
		out[g.name] = nums
	}
	return out
}

func TestGroupPRs_ByRepoIncludingEmptyWorkspacesInPositionOrder(t *testing.T) {
	past := uint(99)
	s := session.Session{
		Worktrees: []session.SessionWorktree{wt(1, "web-app-api", 2, 20), wt(0, "api-service", 1, 10), wt(2, "data-sync", 3, 30)},
		PullRequests: []git.PullRequest{
			{Number: 4847, RepoID: 1, State: git.PRStateDraft},
			{Number: 2236, RepoID: 2, State: git.PRStateMerged},
			{Number: 4426, RepoID: 2, State: git.PRStateMerged, HeadBranchID: &past},
			{Number: 5000, RepoID: 2, State: git.PRStateOpen},
		},
	}
	gs := groupPRs(s)
	assert.Equal(t, []string{"api-service", "web-app-api", "data-sync"}, []string{gs[0].name, gs[1].name, gs[2].name})
	assert.Equal(t, map[string][]int{
		"api-service": {4847},
		"web-app-api": {5000, 4426, 2236},
		"data-sync":   {},
	}, groupSummary(gs))
}

func TestGroupPRs_FallsBackToHeadBranchWhenRepoUnknown(t *testing.T) {
	b := uint(10)
	s := session.Session{
		Worktrees:    []session.SessionWorktree{wt(0, "utena", 0, 10)},
		PullRequests: []git.PullRequest{{Number: 1, HeadBranchID: &b, State: git.PRStateOpen}},
	}
	assert.Equal(t, map[string][]int{"utena": {1}}, groupSummary(groupPRs(s)))
}

func TestGroupPRs_UnmatchedGoToOther(t *testing.T) {
	s := session.Session{
		Worktrees:    []session.SessionWorktree{wt(0, "utena", 1, 10)},
		PullRequests: []git.PullRequest{{Number: 7, RepoID: 42, State: git.PRStateOpen}},
	}
	gs := groupPRs(s)
	assert.Equal(t, "Other", gs[len(gs)-1].name)
	assert.Equal(t, map[string][]int{"utena": {}, "Other": {7}}, groupSummary(gs))
}

func TestGroupPRs_NoWorktreesUsesWorkspaceDisplay(t *testing.T) {
	s := session.Session{PullRequests: []git.PullRequest{{Number: 3, State: git.PRStateOpen}}}
	assert.Equal(t, map[string][]int{"no workspace": {3}}, groupSummary(groupPRs(s)))
}
