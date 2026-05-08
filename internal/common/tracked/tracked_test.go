package tracked_test

import (
	"github.com/eleonorayaya/utena/internal/common/tracked"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/tmux"
)

var (
	_ tracked.Statuser[tmux.TmuxSessionStatus] = (*tmux.TmuxSession)(nil)
	_ tracked.Statuser[git.WorktreeStatus]     = (*git.Worktree)(nil)
	_ tracked.Statuser[git.BranchStatus]       = (*git.Branch)(nil)

	_ tracked.StatusDeriver[git.BranchStatus] = (*git.Branch)(nil)
)
