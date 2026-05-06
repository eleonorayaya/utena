package git

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/google/go-github/v72/github"
)

var ErrNoGitHubClient = errors.New("github client not configured")

const EventPRUpdated = "git.pr_updated"

type PRUpdatedEvent struct {
	PullRequest *PullRequest
	Previous    *PullRequest
	Repo        *Repo
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

func (s *GitService) ListAllBranches(ctx context.Context, repoPath string) ([]BranchRef, error) {
	return s.cli.listAllBranches(ctx, repoPath)
}

func (s *GitService) FetchOrigin(ctx context.Context, repoPath string) error {
	return s.cli.fetchOrigin(ctx, repoPath)
}

func (s *GitService) Pull(ctx context.Context, repoPath string, branch string) error {
	return s.cli.pull(ctx, repoPath, branch)
}

func (s *GitService) Fetch(ctx context.Context, repoPath string, branch string) error {
	return s.cli.fetch(ctx, repoPath, branch)
}

func (s *GitService) SetupWorktree(ctx context.Context, repoPath string, branchName string, baseBranch string, branchID uint, repoID uint) (bool, string, error) {
	creatingNew := baseBranch != ""

	exists, err := s.cli.hasBranch(ctx, repoPath, branchName)
	if err != nil {
		return false, "", fmt.Errorf("failed to check branch %q: %v", branchName, err)
	}
	if creatingNew && exists {
		return false, "", fmt.Errorf("branch %q already exists; use it as an existing branch instead", branchName)
	}
	if !creatingNew && !exists {
		return false, "", fmt.Errorf("branch %q does not exist; provide a base branch to create it", branchName)
	}

	wtPath := s.cli.worktreePath(repoPath, branchName)

	alreadyExists, err := s.cli.validateWorktree(ctx, wtPath, branchName)
	if err != nil {
		return false, "", err
	}
	if alreadyExists {
		s.ensureWorktreeRecord(branchID, repoID, wtPath)
		return false, wtPath, nil
	}

	var path string
	if creatingNew {
		path, err = s.cli.createWorktree(ctx, repoPath, branchName, baseBranch)
		if err != nil {
			return false, "", fmt.Errorf("failed to create worktree: %v", err)
		}
	} else {
		path, err = s.cli.checkoutWorktree(ctx, repoPath, branchName)
		if err != nil {
			return false, "", fmt.Errorf("failed to checkout worktree: %v", err)
		}
	}

	s.ensureWorktreeRecord(branchID, repoID, path)
	return true, path, nil
}

func (s *GitService) ensureWorktreeRecord(branchID uint, repoID uint, path string) {
	if branchID == 0 || repoID == 0 {
		return
	}
	_ = s.worktreeStore.DeleteByBranchID(branchID)
	_ = s.worktreeStore.Add(&Worktree{Path: path, BranchID: branchID, RepoID: repoID})
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

func (s *GitService) MigrateToBare(ctx context.Context, workspacePath string) error {
	return s.cli.migrateToBare(ctx, workspacePath)
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

func (s *GitService) DeleteWorktreesByRepoID(repoID uint) error {
	return s.worktreeStore.DeleteByRepoID(repoID)
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
	if wt != nil && branch.Name != s.cli.defaultBranch(ctx, repoPath) {
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

func (s *GitService) syncGitHubPR(ctx context.Context, ghPR *github.PullRequest, repo *Repo) error {
	headRef := ghPR.GetHead().GetRef()
	branch, _ := s.branchStore.GetByNameAndRepo(headRef, repo.ID)
	if branch == nil {
		branch = &Branch{Name: headRef, RepoID: repo.ID, ExistsRemote: true}
		if err := s.branchStore.Upsert(branch); err != nil {
			return fmt.Errorf("failed to upsert branch %s: %w", headRef, err)
		}
	}

	existing, _ := s.prStore.GetByRepoAndNumber(repo.ID, ghPR.GetNumber())
	pr := ghPRToPullRequest(ghPR, repo.ID, branch.ID, s.currentUser)

	var previous *PullRequest
	if existing != nil {
		previous = &PullRequest{}
		*previous = *existing
		pr.Model = existing.Model
		if err := s.prStore.Update(pr); err != nil {
			return fmt.Errorf("failed to update PR #%d: %w", ghPR.GetNumber(), err)
		}
	} else {
		if err := s.prStore.Upsert(pr); err != nil {
			return fmt.Errorf("failed to upsert PR #%d: %w", ghPR.GetNumber(), err)
		}
	}
	if s.eventBus != nil && prChanged(previous, pr) {
		if err := s.eventBus.Publish(ctx, eventbus.Event{
			Type: EventPRUpdated,
			Data: PRUpdatedEvent{PullRequest: pr, Previous: previous, Repo: repo},
		}); err != nil {
			slog.Warn("failed to publish PR-updated event", "pr", pr.Number, "error", err)
		}
	}
	return nil
}

func prChanged(previous *PullRequest, current *PullRequest) bool {
	if previous == nil {
		return true
	}
	return previous.State != current.State ||
		previous.Title != current.Title ||
		previous.IsAssignedToMe != current.IsAssignedToMe
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
	for _, ghPR := range rawPRs {
		if err := s.syncGitHubPR(ctx, ghPR, repo); err != nil {
			slog.Warn("failed to sync PR", "number", ghPR.GetNumber(), "error", err)
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("SyncRepoPRs: %d errors (first: %w)", len(errs), errs[0])
	}
	return nil
}

func ghPRToPullRequest(ghPR *github.PullRequest, repoID uint, branchID uint, currentUser string) *PullRequest {
	assigned := false
	for _, a := range ghPR.Assignees {
		if a.GetLogin() == currentUser {
			assigned = true
			break
		}
	}
	if !assigned {
		for _, r := range ghPR.RequestedReviewers {
			if r.GetLogin() == currentUser {
				assigned = true
				break
			}
		}
	}
	state := PRStateOpen
	if ghPR.MergedAt != nil {
		state = PRStateMerged
	} else if ghPR.GetState() == "closed" {
		state = PRStateClosed
	} else if ghPR.GetDraft() {
		state = PRStateDraft
	}
	return &PullRequest{
		RepoID:         repoID,
		Number:         ghPR.GetNumber(),
		HeadBranchID:   &branchID,
		Title:          ghPR.GetTitle(),
		State:          state,
		IsAssignedToMe: assigned,
		HTMLURL:        ghPR.GetHTMLURL(),
		AuthorLogin:    ghPR.GetUser().GetLogin(),
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
