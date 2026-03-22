package session

type ResourceStatus string

const (
	ResourcePending  ResourceStatus = "pending"
	ResourceCreating ResourceStatus = "creating"
	ResourceReady    ResourceStatus = "ready"
	ResourceFailed   ResourceStatus = "failed"
	ResourceRemoved  ResourceStatus = "removed"
)

type ResourceState struct {
	Status ResourceStatus `json:"status"`
	Error  string         `json:"error,omitempty"`
}

type Resources struct {
	Branch       *ResourceState `json:"branch,omitempty"`
	Worktree     *ResourceState `json:"worktree,omitempty"`
	WorktreeInit *ResourceState `json:"worktree_init,omitempty"`
	Tmux         *ResourceState `json:"tmux,omitempty"`
}

func (r *Resources) AllReady() bool {
	if r == nil {
		return true
	}
	for _, rs := range []*ResourceState{r.Branch, r.Worktree, r.WorktreeInit, r.Tmux} {
		if rs != nil && rs.Status != ResourceReady {
			return false
		}
	}
	return true
}

func (r *Resources) FirstError() string {
	if r == nil {
		return ""
	}
	for _, rs := range []*ResourceState{r.Branch, r.Worktree, r.WorktreeInit, r.Tmux} {
		if rs != nil && rs.Error != "" {
			return rs.Error
		}
	}
	return ""
}
