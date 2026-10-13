package session

import (
	"time"

	"github.com/eleonorayaya/utena/internal/git"
	"gorm.io/gorm"
)

type SessionBranch struct {
	gorm.Model
	SessionID   uint        `json:"session_id" gorm:"uniqueIndex:idx_session_branch;index;not null"`
	WorktreeID  uint        `json:"worktree_id" gorm:"index;not null"`
	BranchID    uint        `json:"branch_id" gorm:"uniqueIndex:idx_session_branch;index;not null"`
	FirstSeenAt time.Time   `json:"first_seen_at"`
	Branch      *git.Branch `json:"branch,omitempty" gorm:"foreignKey:BranchID;constraint:OnDelete:CASCADE"`
}

func (s *Session) currentBranchIDs() map[uint]struct{} {
	current := make(map[uint]struct{}, len(s.Worktrees))
	for i := range s.Worktrees {
		if wt := s.Worktrees[i].Worktree; wt != nil && wt.BranchID != 0 {
			current[wt.BranchID] = struct{}{}
		}
	}
	return current
}

func (s *Session) BranchIDs() []uint {
	seen := make(map[uint]struct{})
	out := make([]uint, 0, len(s.Worktrees)+len(s.Branches))
	add := func(id uint) {
		if id == 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for i := range s.Worktrees {
		if wt := s.Worktrees[i].Worktree; wt != nil {
			add(wt.BranchID)
		}
	}
	for i := range s.Branches {
		add(s.Branches[i].BranchID)
	}
	return out
}

func (s *Session) PastBranches() []git.Branch {
	current := s.currentBranchIDs()
	var out []git.Branch
	for i := range s.Branches {
		sb := s.Branches[i]
		if sb.Branch == nil {
			continue
		}
		if _, ok := current[sb.BranchID]; ok {
			continue
		}
		out = append(out, *sb.Branch)
	}
	return out
}

func (s *Session) FilterPRs(prs []git.PullRequest) []git.PullRequest {
	owned := make(map[uint]struct{})
	for _, id := range s.BranchIDs() {
		owned[id] = struct{}{}
	}
	var out []git.PullRequest
	for _, pr := range prs {
		if pr.HeadBranchID == nil {
			continue
		}
		if _, ok := owned[*pr.HeadBranchID]; ok {
			out = append(out, pr)
		}
	}
	return out
}

func (s *Session) WorkspaceIDs() []uint {
	seen := make(map[uint]struct{}, len(s.Worktrees))
	out := make([]uint, 0, len(s.Worktrees))
	for i := range s.Worktrees {
		ws := s.Worktrees[i].Workspace
		if ws == nil {
			continue
		}
		if _, ok := seen[ws.ID]; ok {
			continue
		}
		seen[ws.ID] = struct{}{}
		out = append(out, ws.ID)
	}
	return out
}

func (s *Session) CollectPRs(byWorkspace map[uint][]git.PullRequest) []git.PullRequest {
	var all []git.PullRequest
	for _, id := range s.WorkspaceIDs() {
		all = append(all, byWorkspace[id]...)
	}
	return s.FilterPRs(all)
}
