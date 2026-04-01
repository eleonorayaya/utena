package git

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
)

const (
	EventPRDiscovered   = "git.pr_discovered"
	EventPRStateChanged = "git.pr_state_changed"
)

type PRDiscoveredEvent struct {
	PullRequest *PullRequest
	Repo        *Repo
}

type PRStateChangedEvent struct {
	PullRequest *PullRequest
	OldState    PRState
	NewState    PRState
}

type GitService struct {
	cli           *gitCLI
	repoStore     *RepoStore
	branchStore   *BranchStore
	worktreeStore *WorktreeStore
	prStore       *PRStore
	githubClient  GitHubClient
	eventBus      eventbus.EventBus
}

type GitServiceOption func(*GitService)

func WithGitHubClient(client GitHubClient) GitServiceOption {
	return func(s *GitService) { s.githubClient = client }
}

func WithEventBus(bus eventbus.EventBus) GitServiceOption {
	return func(s *GitService) { s.eventBus = bus }
}

func NewGitService(database db.Database, opts ...GitServiceOption) *GitService {
	s := &GitService{
		cli:           newGitCLI(),
		repoStore:     NewRepoStore(database),
		branchStore:   NewBranchStore(database),
		worktreeStore: NewWorktreeStore(database),
		prStore:       NewPRStore(database),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *GitService) ListBranches(ctx context.Context, repoPath string) ([]string, error) {
	return s.cli.listBranches(ctx, repoPath)
}

func (s *GitService) Pull(ctx context.Context, repoPath string, branch string) error {
	return s.cli.pull(ctx, repoPath, branch)
}

func (s *GitService) CreateWorktree(ctx context.Context, repoPath string, branchName string, baseBranch string) (string, error) {
	return s.cli.createWorktree(ctx, repoPath, branchName, baseBranch)
}

func (s *GitService) CheckoutWorktree(ctx context.Context, repoPath string, branch string) (string, error) {
	return s.cli.checkoutWorktree(ctx, repoPath, branch)
}

func (s *GitService) CurrentBranch(ctx context.Context, repoPath string) (string, error) {
	return s.cli.currentBranch(ctx, repoPath)
}

func (s *GitService) IsDirty(ctx context.Context, repoPath string) (bool, error) {
	return s.cli.isDirty(ctx, repoPath)
}

func (s *GitService) HasRemoteBranch(ctx context.Context, repoPath string, branch string) (bool, error) {
	return s.cli.hasRemoteBranch(ctx, repoPath, branch)
}

func (s *GitService) HasBranch(ctx context.Context, repoPath string, branch string) (bool, error) {
	return s.cli.hasBranch(ctx, repoPath, branch)
}

func (s *GitService) ValidateWorktree(ctx context.Context, worktreePath string, expectedBranch string) (bool, error) {
	return s.cli.validateWorktree(ctx, worktreePath, expectedBranch)
}

func (s *GitService) WorktreePath(repoPath string, branch string) string {
	return s.cli.worktreePath(repoPath, branch)
}

func (s *GitService) RemoveWorktree(ctx context.Context, repoPath string, worktreePath string) error {
	return s.cli.removeWorktree(ctx, repoPath, worktreePath)
}

func (s *GitService) DeleteBranch(ctx context.Context, repoPath string, branchName string) error {
	return s.cli.deleteBranch(ctx, repoPath, branchName)
}

func (s *GitService) FindOrCreateRepo(ctx context.Context, repoPath string) (*Repo, error) {
	existing, err := s.repoStore.GetByPath(repoPath)
	if err == nil {
		return existing, nil
	}
	url, err := s.cli.remoteURL(ctx, repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get remote URL: %w", err)
	}
	owner, name, err := s.cli.parseRepoFullName(url)
	if err != nil {
		return nil, fmt.Errorf("failed to parse repo name: %w", err)
	}
	repo := &Repo{
		Path:     repoPath,
		FullName: owner + "/" + name,
	}
	if err := s.repoStore.Upsert(repo); err != nil {
		return nil, err
	}
	return repo, nil
}

func (s *GitService) GetRepo(id uint) (*Repo, error) {
	return s.repoStore.GetByID(id)
}

func (s *GitService) GetRepoByPath(path string) (*Repo, error) {
	return s.repoStore.GetByPath(path)
}

func (s *GitService) FindOrCreateBranch(ctx context.Context, name string, repoID uint) (*Branch, error) {
	existing, err := s.branchStore.GetByNameAndRepo(name, repoID)
	if err == nil {
		return existing, nil
	}
	branch := &Branch{Name: name, RepoID: repoID}
	if err := s.branchStore.Upsert(branch); err != nil {
		return nil, err
	}
	return branch, nil
}

func (s *GitService) CreateBranch(ctx context.Context, repoPath string, branchName string, baseBranch string, repoID uint) (*Branch, *Worktree, error) {
	baseBranchRecord, _ := s.branchStore.GetByNameAndRepo(baseBranch, repoID)

	wtPath, err := s.cli.createWorktree(ctx, repoPath, branchName, baseBranch)
	if err != nil {
		return nil, nil, err
	}

	var baseBranchID *uint
	if baseBranchRecord != nil {
		baseBranchID = &baseBranchRecord.ID
	}
	branch := &Branch{
		Name:         branchName,
		RepoID:       repoID,
		BaseBranchID: baseBranchID,
		ExistsLocal:  true,
	}
	if err := s.branchStore.Upsert(branch); err != nil {
		return nil, nil, err
	}

	wt := &Worktree{
		Path:     wtPath,
		BranchID: branch.ID,
		RepoID:   repoID,
	}
	if err := s.worktreeStore.Add(wt); err != nil {
		return nil, nil, err
	}

	return branch, wt, nil
}

func (s *GitService) GetBranch(id uint) (*Branch, error) {
	return s.branchStore.GetByID(id)
}

func (s *GitService) ListBranchesByRepo(repoID uint) []Branch {
	return s.branchStore.ListByRepo(repoID)
}

func (s *GitService) EnsureWorktree(ctx context.Context, branch *Branch, repoPath string) (string, error) {
	existing, err := s.worktreeStore.GetByBranchID(branch.ID)
	if err == nil && existing != nil {
		return existing.Path, nil
	}

	if branch.ExistsRemote {
		_ = s.cli.pull(ctx, repoPath, branch.Name)
	}

	wtPath, err := s.cli.checkoutWorktree(ctx, repoPath, branch.Name)
	if err != nil {
		return "", err
	}

	wt := &Worktree{
		Path:     wtPath,
		BranchID: branch.ID,
		RepoID:   branch.RepoID,
	}
	if err := s.worktreeStore.Add(wt); err != nil {
		return "", err
	}

	return wtPath, nil
}

func (s *GitService) GetStartDir(branch *Branch, repoPath string) string {
	wt, err := s.worktreeStore.GetByBranchID(branch.ID)
	if err == nil && wt != nil {
		return wt.Path
	}
	return repoPath
}

func (s *GitService) HasWorktree(branchID uint) bool {
	wt, _ := s.worktreeStore.GetByBranchID(branchID)
	return wt != nil
}

func (s *GitService) IsHealthy(ctx context.Context, branch *Branch, repoPath string) bool {
	wt, _ := s.worktreeStore.GetByBranchID(branch.ID)
	if wt == nil {
		return true
	}
	valid, err := s.cli.validateWorktree(ctx, wt.Path, branch.Name)
	if err != nil || !valid {
		return false
	}
	return true
}

func (s *GitService) SyncBranch(ctx context.Context, branch *Branch, repoPath string) error {
	existsLocal, _ := s.cli.hasBranch(ctx, repoPath, branch.Name)
	existsRemote, _ := s.cli.hasRemoteBranch(ctx, repoPath, branch.Name)

	isDirty := false
	wtPath := s.cli.worktreePath(repoPath, branch.Name)

	wt, _ := s.worktreeStore.GetByBranchID(branch.ID)

	if existsLocal {
		if valid, _ := s.cli.validateWorktree(ctx, wtPath, branch.Name); valid {
			if wt == nil {
				wt = &Worktree{Path: wtPath, BranchID: branch.ID, RepoID: branch.RepoID}
				_ = s.worktreeStore.Add(wt)
			}
			dirty, _ := s.cli.isDirty(ctx, wtPath)
			isDirty = dirty
		} else if wt != nil {
			_ = s.worktreeStore.Delete(wt.ID)
		}
	} else if wt != nil {
		_ = s.worktreeStore.Delete(wt.ID)
	}

	branch.ExistsLocal = existsLocal
	branch.ExistsRemote = existsRemote
	branch.IsDirty = isDirty
	return s.branchStore.Update(branch)
}

func (s *GitService) CleanupBranch(ctx context.Context, branch *Branch, repoPath string, deleteBranch bool) error {
	wt, _ := s.worktreeStore.GetByBranchID(branch.ID)
	if wt != nil {
		if err := s.cli.removeWorktree(ctx, repoPath, wt.Path); err != nil {
			return err
		}
		_ = s.worktreeStore.Delete(wt.ID)
	}
	if deleteBranch {
		if err := s.cli.deleteBranch(ctx, repoPath, branch.Name); err != nil {
			return err
		}
		branch.ExistsLocal = false
		return s.branchStore.Update(branch)
	}
	return nil
}

func (s *GitService) RepoStore() *RepoStore         { return s.repoStore }
func (s *GitService) BranchStore() *BranchStore     { return s.branchStore }
func (s *GitService) WorktreeStore() *WorktreeStore { return s.worktreeStore }
func (s *GitService) PRStore() *PRStore             { return s.prStore }

func (s *GitService) GetPR(id uint) (*PullRequest, error) {
	return s.prStore.GetByID(id)
}

func (s *GitService) GetPRsForBranch(branchID uint) []PullRequest {
	return s.prStore.ListByBranch(branchID)
}

func (s *GitService) SearchPRs(repoID uint, state PRState) []PullRequest {
	if state != "" {
		return s.prStore.ListByState(repoID, state)
	}
	return s.prStore.ListByRepo(repoID)
}

func (s *GitService) GetPRDiff(ctx context.Context, prID uint) (*Diff, error) {
	pr, err := s.prStore.GetByID(prID)
	if err != nil {
		return nil, err
	}
	repo, err := s.repoStore.GetByID(pr.RepoID)
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(repo.FullName, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo full name: %s", repo.FullName)
	}
	if s.githubClient == nil {
		return nil, fmt.Errorf("GitHub client not available")
	}
	raw, err := s.githubClient.GetPRDiff(ctx, parts[0], parts[1], pr.Number)
	if err != nil {
		return nil, err
	}
	return ParseUnifiedDiff(raw)
}

func (s *GitService) SyncRepoPRs(ctx context.Context, repo *Repo) error {
	if s.githubClient == nil {
		return nil
	}
	parts := strings.SplitN(repo.FullName, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid repo full name: %s", repo.FullName)
	}
	rawPRs, err := s.githubClient.ListRepoPRs(ctx, parts[0], parts[1])
	if err != nil {
		return err
	}
	for _, raw := range rawPRs {
		branch, _ := s.branchStore.GetByNameAndRepo(raw.Head.Ref, repo.ID)
		if branch == nil {
			branch = &Branch{Name: raw.Head.Ref, RepoID: repo.ID, ExistsRemote: true}
			if err := s.branchStore.Upsert(branch); err != nil {
				continue
			}
		}

		existing, _ := s.prStore.GetByRepoAndNumber(repo.ID, raw.Number)
		pr := &PullRequest{
			RepoID:       repo.ID,
			Number:       raw.Number,
			HeadBranchID: &branch.ID,
			Title:        raw.Title,
			State:        raw.ToPRState(),
			IsDraft:      raw.Draft,
			HTMLURL:      raw.HTMLURL,
			AuthorLogin:  raw.User.Login,
		}

		if existing != nil {
			oldState := existing.State
			pr.Model = existing.Model
			if err := s.prStore.Update(pr); err != nil {
				continue
			}
			if oldState != pr.State && s.eventBus != nil {
				s.eventBus.Publish(ctx, eventbus.Event{
					Type: EventPRStateChanged,
					Data: PRStateChangedEvent{PullRequest: pr, OldState: oldState, NewState: pr.State},
				})
			}
		} else {
			if err := s.prStore.Upsert(pr); err != nil {
				continue
			}
			if s.eventBus != nil {
				s.eventBus.Publish(ctx, eventbus.Event{
					Type: EventPRDiscovered,
					Data: PRDiscoveredEvent{PullRequest: pr, Repo: repo},
				})
			}
		}
	}
	return nil
}

func (s *GitService) SyncAssignedPRs(ctx context.Context) ([]PullRequest, error) {
	if s.githubClient == nil {
		return nil, nil
	}
	rawPRs, err := s.githubClient.ListAssignedPRs(ctx)
	if err != nil {
		return nil, err
	}
	var discovered []PullRequest
	for _, raw := range rawPRs {
		if raw.Head.Repo == nil {
			continue
		}
		repo, err := s.repoStore.GetByFullName(raw.Head.Repo.FullName)
		if err != nil {
			continue
		}
		existing, _ := s.prStore.GetByRepoAndNumber(repo.ID, raw.Number)
		if existing != nil {
			continue
		}
		branch, _ := s.branchStore.GetByNameAndRepo(raw.Head.Ref, repo.ID)
		if branch == nil {
			branch = &Branch{Name: raw.Head.Ref, RepoID: repo.ID, ExistsRemote: true}
			s.branchStore.Upsert(branch)
		}
		pr := &PullRequest{
			RepoID:       repo.ID,
			Number:       raw.Number,
			HeadBranchID: &branch.ID,
			Title:        raw.Title,
			State:        raw.ToPRState(),
			IsDraft:      raw.Draft,
			HTMLURL:      raw.HTMLURL,
			AuthorLogin:  raw.User.Login,
		}
		s.prStore.Upsert(pr)
		discovered = append(discovered, *pr)
		if s.eventBus != nil {
			s.eventBus.Publish(ctx, eventbus.Event{
				Type: EventPRDiscovered,
				Data: PRDiscoveredEvent{PullRequest: pr, Repo: repo},
			})
		}
	}
	return discovered, nil
}

func (s *GitService) SyncBranches(ctx context.Context, repoID uint, repoPath string) error {
	branches := s.branchStore.ListByRepo(repoID)
	for i := range branches {
		if err := s.SyncBranch(ctx, &branches[i], repoPath); err != nil {
			slog.Warn("failed to sync branch", "branch", branches[i].Name, "error", err)
		}
	}
	return nil
}
