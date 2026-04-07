package git

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
)

var ErrNoGitHubClient = errors.New("github client not configured")

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
	currentUser   string
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
	baseBranchRecord, err := s.branchStore.GetByNameAndRepo(baseBranch, repoID)
	if err != nil && !errors.Is(err, ErrBranchNotFound) {
		return nil, nil, fmt.Errorf("failed to look up base branch %s: %w", baseBranch, err)
	}

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
	if err == nil {
		return existing.Path, nil
	}
	if !errors.Is(err, ErrWorktreeNotFound) {
		return "", fmt.Errorf("failed to check existing worktree: %w", err)
	}

	if branch.ExistsRemote {
		if err := s.cli.pull(ctx, repoPath, branch.Name); err != nil {
			slog.Warn("failed to pull branch before worktree checkout", "branch", branch.Name, "error", err)
		}
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
	if err == nil {
		return wt.Path
	}
	return repoPath
}

func (s *GitService) HasWorktree(branchID uint) bool {
	_, err := s.worktreeStore.GetByBranchID(branchID)
	return err == nil
}

func (s *GitService) IsHealthy(ctx context.Context, branch *Branch, repoPath string) bool {
	wt, err := s.worktreeStore.GetByBranchID(branch.ID)
	if errors.Is(err, ErrWorktreeNotFound) {
		return true
	}
	if err != nil {
		return false
	}
	valid, err := s.cli.validateWorktree(ctx, wt.Path, branch.Name)
	if err != nil || !valid {
		return false
	}
	return true
}

func (s *GitService) SyncBranch(ctx context.Context, branch *Branch, repoPath string) error {
	existsLocal, err := s.cli.hasBranch(ctx, repoPath, branch.Name)
	if err != nil {
		return fmt.Errorf("failed to check local branch %s: %w", branch.Name, err)
	}
	existsRemote, err := s.cli.hasRemoteBranch(ctx, repoPath, branch.Name)
	if err != nil {
		return fmt.Errorf("failed to check remote branch %s: %w", branch.Name, err)
	}

	isDirty := false
	wtPath := s.cli.worktreePath(repoPath, branch.Name)

	wt, wtErr := s.worktreeStore.GetByBranchID(branch.ID)
	if wtErr != nil && !errors.Is(wtErr, ErrWorktreeNotFound) {
		return fmt.Errorf("failed to look up worktree for branch %s: %w", branch.Name, wtErr)
	}

	if existsLocal {
		if valid, err := s.cli.validateWorktree(ctx, wtPath, branch.Name); err != nil {
			slog.Warn("failed to validate worktree", "path", wtPath, "error", err)
		} else if valid {
			if errors.Is(wtErr, ErrWorktreeNotFound) {
				wt = &Worktree{Path: wtPath, BranchID: branch.ID, RepoID: branch.RepoID}
				if err := s.worktreeStore.Add(wt); err != nil {
					slog.Warn("failed to add worktree record", "path", wtPath, "error", err)
				}
			}
			if dirty, err := s.cli.isDirty(ctx, wtPath); err != nil {
				slog.Warn("failed to check dirty state", "path", wtPath, "error", err)
			} else {
				isDirty = dirty
			}
		} else if wt != nil {
			if err := s.worktreeStore.Delete(wt.ID); err != nil {
				slog.Warn("failed to delete stale worktree record", "id", wt.ID, "error", err)
			}
		}
	} else if wt != nil {
		if err := s.worktreeStore.Delete(wt.ID); err != nil {
			slog.Warn("failed to delete worktree record for missing local branch", "id", wt.ID, "error", err)
		}
	}

	branch.ExistsLocal = existsLocal
	branch.ExistsRemote = existsRemote
	branch.IsDirty = isDirty
	return s.branchStore.Update(branch)
}

func (s *GitService) CleanupBranch(ctx context.Context, branch *Branch, repoPath string, deleteBranch bool) error {
	wt, err := s.worktreeStore.GetByBranchID(branch.ID)
	if err != nil && !errors.Is(err, ErrWorktreeNotFound) {
		return fmt.Errorf("failed to look up worktree: %w", err)
	}
	if wt != nil {
		if err := s.cli.removeWorktree(ctx, repoPath, wt.Path); err != nil {
			return err
		}
		if err := s.worktreeStore.Delete(wt.ID); err != nil {
			return fmt.Errorf("failed to delete worktree record: %w", err)
		}
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
	owner, name, err := repo.OwnerAndName()
	if err != nil {
		return nil, err
	}
	if s.githubClient == nil {
		return nil, fmt.Errorf("GitHub client not available")
	}
	raw, err := s.githubClient.GetPRDiff(ctx, owner, name, pr.Number)
	if err != nil {
		return nil, err
	}
	return ParseUnifiedDiff(raw)
}

func (s *GitService) syncRawPR(ctx context.Context, raw RawPR, repo *Repo) error {
	branch, _ := s.branchStore.GetByNameAndRepo(raw.Head.Ref, repo.ID)
	if branch == nil {
		branch = &Branch{Name: raw.Head.Ref, RepoID: repo.ID, ExistsRemote: true}
		if err := s.branchStore.Upsert(branch); err != nil {
			return fmt.Errorf("failed to upsert branch %s: %w", raw.Head.Ref, err)
		}
	}

	existing, _ := s.prStore.GetByRepoAndNumber(repo.ID, raw.Number)
	pr := rawPRToPullRequest(raw, repo.ID, branch.ID, s.currentUser)

	if existing != nil {
		oldState := existing.State
		pr.Model = existing.Model
		if err := s.prStore.Update(pr); err != nil {
			return fmt.Errorf("failed to update PR #%d: %w", raw.Number, err)
		}
		if oldState != pr.State && s.eventBus != nil {
			s.eventBus.Publish(ctx, eventbus.Event{
				Type: EventPRStateChanged,
				Data: PRStateChangedEvent{PullRequest: pr, OldState: oldState, NewState: pr.State},
			})
		}
	} else {
		if err := s.prStore.Upsert(pr); err != nil {
			return fmt.Errorf("failed to upsert PR #%d: %w", raw.Number, err)
		}
		if s.eventBus != nil {
			s.eventBus.Publish(ctx, eventbus.Event{
				Type: EventPRDiscovered,
				Data: PRDiscoveredEvent{PullRequest: pr, Repo: repo},
			})
		}
	}
	return nil
}

func (s *GitService) SyncRepoPRs(ctx context.Context, repo *Repo) error {
	if s.githubClient == nil {
		return ErrNoGitHubClient
	}
	owner, name, err := repo.OwnerAndName()
	if err != nil {
		return err
	}
	rawPRs, err := s.githubClient.ListRepoPRs(ctx, owner, name)
	if err != nil {
		return err
	}
	var errs []error
	for _, raw := range rawPRs {
		if err := s.syncRawPR(ctx, raw, repo); err != nil {
			slog.Warn("failed to sync PR", "number", raw.Number, "error", err)
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("SyncRepoPRs: %d errors (first: %w)", len(errs), errs[0])
	}
	return nil
}

func (s *GitService) SyncAssignedPRs(ctx context.Context) ([]PullRequest, error) {
	if s.githubClient == nil {
		return nil, ErrNoGitHubClient
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
			slog.Debug("skipping assigned PR for untracked repo", "repo", raw.Head.Repo.FullName)
			continue
		}
		existing, _ := s.prStore.GetByRepoAndNumber(repo.ID, raw.Number)
		if existing != nil {
			continue
		}
		branch, _ := s.branchStore.GetByNameAndRepo(raw.Head.Ref, repo.ID)
		if branch == nil {
			branch = &Branch{Name: raw.Head.Ref, RepoID: repo.ID, ExistsRemote: true}
			if err := s.branchStore.Upsert(branch); err != nil {
				slog.Warn("failed to upsert branch for assigned PR", "branch", raw.Head.Ref, "error", err)
				continue
			}
		}
		pr := rawPRToPullRequest(raw, repo.ID, branch.ID, s.currentUser)
		if err := s.prStore.Upsert(pr); err != nil {
			slog.Warn("failed to upsert assigned PR", "number", raw.Number, "error", err)
			continue
		}
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

func rawPRToPullRequest(raw RawPR, repoID uint, branchID uint, currentUser string) *PullRequest {
	assigned := false
	for _, a := range raw.Assignees {
		if a.Login == currentUser {
			assigned = true
			break
		}
	}
	return &PullRequest{
		RepoID:         repoID,
		Number:         raw.Number,
		HeadBranchID:   &branchID,
		Title:          raw.Title,
		State:          raw.ToPRState(),
		IsDraft:        raw.Draft,
		IsAssignedToMe: assigned,
		HTMLURL:        raw.HTMLURL,
		AuthorLogin:    raw.User.Login,
	}
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
