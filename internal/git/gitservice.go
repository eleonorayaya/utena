package git

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

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

func (s *GitService) PushSetUpstream(ctx context.Context, repoPath string, branchName string) error {
	return s.cli.pushSetUpstream(ctx, repoPath, branchName)
}

func (s *GitService) SetupWorktreeAt(ctx context.Context, repoPath string, branchName string, baseBranch string, branchID uint, repoID uint, destPath string) (bool, string, error) {
	creatingNew := baseBranch != ""

	exists, err := s.cli.hasBranch(ctx, repoPath, branchName)
	if err != nil {
		return false, "", fmt.Errorf("failed to check branch %q: %v", branchName, err)
	}
	if creatingNew && exists {
		return false, "", fmt.Errorf("branch %q already exists; use it as an existing branch instead", branchName)
	}
	if !creatingNew && !exists {
		hasRemote, remErr := s.cli.hasRemoteBranch(ctx, repoPath, branchName)
		if remErr != nil {
			return false, "", fmt.Errorf("failed to check remote branch %q: %v", branchName, remErr)
		}
		if !hasRemote {
			return false, "", fmt.Errorf("branch %q does not exist locally or on remote", branchName)
		}
		if fetchErr := s.cli.fetch(ctx, repoPath, branchName); fetchErr != nil {
			return false, "", fmt.Errorf("failed to fetch branch %q: %v", branchName, fetchErr)
		}
	}

	wtPath := destPath
	if wtPath == "" {
		wtPath = s.cli.worktreePath(repoPath, branchName)
	}

	alreadyExists, err := s.cli.validateWorktree(wtPath)
	if err != nil {
		return false, "", err
	}
	if alreadyExists {
		s.ensureWorktreeRecord(branchID, repoID, wtPath)
		return false, wtPath, nil
	}

	var path string
	if creatingNew {
		path, err = s.cli.createWorktree(ctx, repoPath, branchName, baseBranch, destPath)
		if err != nil {
			return false, "", fmt.Errorf("failed to create worktree: %v", err)
		}
	} else {
		path, err = s.cli.checkoutWorktree(ctx, repoPath, branchName, destPath)
		if err != nil {
			return false, "", fmt.Errorf("failed to checkout worktree: %v", err)
		}
	}

	s.ensureWorktreeRecord(branchID, repoID, path)
	return true, path, nil
}

func (s *GitService) RegisterPendingWorktree(branchID uint, repoID uint, path string) (*Worktree, error) {
	if branchID == 0 {
		return nil, fmt.Errorf("branchID is required")
	}
	existing, err := s.worktreeStore.GetByBranchID(branchID)
	if err == nil {
		changed := false
		if existing.Path != path {
			existing.Path = path
			changed = true
		}
		if existing.RepoID != repoID && repoID != 0 {
			existing.RepoID = repoID
			changed = true
		}
		if existing.Status == "" {
			existing.Status = WorktreeStatusPending
			changed = true
		}
		if changed {
			if updateErr := s.worktreeStore.Update(existing); updateErr != nil {
				return nil, fmt.Errorf("failed to update worktree record: %w", updateErr)
			}
		}
		return existing, nil
	}
	if !errors.Is(err, ErrWorktreeNotFound) {
		return nil, fmt.Errorf("failed to look up worktree: %w", err)
	}
	wt := &Worktree{
		Path:     path,
		BranchID: branchID,
		RepoID:   repoID,
		Status:   WorktreeStatusPending,
	}
	if err := s.worktreeStore.Add(wt); err != nil {
		return nil, err
	}
	return wt, nil
}

func (s *GitService) MarkWorktreeMissing(wt *Worktree) error {
	if wt == nil {
		return fmt.Errorf("worktree is nil")
	}
	if wt.Status == WorktreeStatusMissing {
		return nil
	}
	wt.Status = WorktreeStatusMissing
	return s.worktreeStore.Update(wt)
}

func (s *GitService) UpdateWorktree(wt *Worktree) error {
	if wt == nil {
		return fmt.Errorf("worktree is nil")
	}
	return s.worktreeStore.Update(wt)
}

func (s *GitService) GetWorktree(id uint) (*Worktree, error) {
	return s.worktreeStore.GetByID(id)
}

func (s *GitService) GetWorktreeByBranchID(branchID uint) (*Worktree, error) {
	return s.worktreeStore.GetByBranchID(branchID)
}

func (s *GitService) ensureWorktreeRecord(branchID uint, repoID uint, path string) {
	if branchID == 0 || repoID == 0 {
		return
	}
	existing, err := s.worktreeStore.GetByBranchID(branchID)
	if err == nil {
		existing.Path = path
		existing.RepoID = repoID
		existing.Status = WorktreeStatusPresent
		if updateErr := s.worktreeStore.Update(existing); updateErr != nil {
			slog.Warn("failed to update worktree record", "branch_id", branchID, "error", updateErr)
		}
		return
	}
	if !errors.Is(err, ErrWorktreeNotFound) {
		slog.Warn("failed to look up worktree record", "branch_id", branchID, "error", err)
		return
	}
	_ = s.worktreeStore.Add(&Worktree{Path: path, BranchID: branchID, RepoID: repoID, Status: WorktreeStatusPresent})
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

func (s *GitService) ValidateWorktree(worktreePath string) (bool, error) {
	return s.cli.validateWorktree(worktreePath)
}

func (s *GitService) WorktreePath(repoPath string, branch string) string {
	return s.cli.worktreePath(repoPath, branch)
}

func (s *GitService) RemoveWorktree(ctx context.Context, repoPath string, worktreePath string) error {
	return s.cli.removeWorktree(ctx, repoPath, worktreePath)
}

func (s *GitService) PruneWorktrees(ctx context.Context, repoPath string) error {
	return s.cli.pruneWorktrees(ctx, repoPath)
}

func (s *GitService) MigrateToBare(ctx context.Context, workspacePath string, onProgress func(string)) error {
	return s.cli.migrateToBare(ctx, workspacePath, onProgress)
}

func (s *GitService) CloneBareWorkspace(ctx context.Context, remoteURL, workspacePath string, onProgress func(string)) error {
	return s.cli.cloneBareWorkspace(ctx, remoteURL, workspacePath, onProgress)
}

func (s *GitService) ParseRepoFullName(remoteURL string) (owner string, repo string, err error) {
	return s.cli.parseRepoFullName(remoteURL)
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
	branch := &Branch{Name: name, RepoID: repoID, Status: BranchStatusPending}
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

	wtPath, err := s.cli.createWorktree(ctx, repoPath, branchName, baseBranch, "")
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
	branch.Status = branch.DeriveStatus()
	if err := s.branchStore.Upsert(branch); err != nil {
		return nil, nil, err
	}

	wt := &Worktree{
		Path:     wtPath,
		BranchID: branch.ID,
		RepoID:   repoID,
		Status:   WorktreeStatusPresent,
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

func (s *GitService) DeleteWorktree(id uint) error {
	return s.worktreeStore.Delete(id)
}

func (s *GitService) IsHealthy(ctx context.Context, branch *Branch, repoPath string) bool {
	wt, err := s.worktreeStore.GetByBranchID(branch.ID)
	if errors.Is(err, ErrWorktreeNotFound) {
		return true
	}
	if err != nil {
		return false
	}
	valid, err := s.cli.validateWorktree(wt.Path)
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

	wt, wtErr := s.worktreeStore.GetByBranchID(branch.ID)
	if wtErr != nil && !errors.Is(wtErr, ErrWorktreeNotFound) {
		return fmt.Errorf("failed to look up worktree for branch %s: %w", branch.Name, wtErr)
	}

	checkPath := s.cli.worktreePath(repoPath, branch.Name)
	if wt != nil && wt.Path != "" {
		checkPath = wt.Path
	}

	if existsLocal {
		if valid, err := s.cli.validateWorktree(checkPath); err != nil {
			slog.Warn("failed to validate worktree", "path", checkPath, "error", err)
		} else if valid {
			if errors.Is(wtErr, ErrWorktreeNotFound) {
				wt = &Worktree{Path: checkPath, BranchID: branch.ID, RepoID: branch.RepoID, Status: WorktreeStatusPresent}
				if err := s.worktreeStore.Add(wt); err != nil {
					slog.Warn("failed to add worktree record", "path", checkPath, "error", err)
				}
			} else if wt.Status != WorktreeStatusPresent {
				wt.Status = WorktreeStatusPresent
				if err := s.worktreeStore.Update(wt); err != nil {
					slog.Warn("failed to mark worktree present", "id", wt.ID, "error", err)
				}
			}
			if dirty, err := s.cli.isDirty(ctx, checkPath); err != nil {
				slog.Warn("failed to check dirty state", "path", checkPath, "error", err)
			} else {
				isDirty = dirty
			}
		} else if wt != nil && wt.Status != WorktreeStatusMissing {
			wt.Status = WorktreeStatusMissing
			if err := s.worktreeStore.Update(wt); err != nil {
				slog.Warn("failed to mark worktree missing", "id", wt.ID, "error", err)
			}
		}
	} else if wt != nil && wt.Status != WorktreeStatusMissing {
		wt.Status = WorktreeStatusMissing
		if err := s.worktreeStore.Update(wt); err != nil {
			slog.Warn("failed to mark worktree missing for missing local branch", "id", wt.ID, "error", err)
		}
	}

	branch.ExistsLocal = existsLocal
	branch.ExistsRemote = existsRemote
	branch.IsDirty = isDirty
	branch.Status = branch.DeriveStatus()
	return s.branchStore.Update(branch)
}

func (s *GitService) CleanupBranch(ctx context.Context, branch *Branch, repoPath string, deleteBranch bool) error {
	if deleteBranch {
		if err := s.cli.deleteBranch(ctx, repoPath, branch.Name); err != nil {
			return err
		}
		branch.ExistsLocal = false
		branch.Status = branch.DeriveStatus()
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
		branch.Status = branch.DeriveStatus()
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
		// the PR sync overwrites every column, so carry the activity
		// watermarks that only SyncPRActivity maintains
		pr.LastReviewID = existing.LastReviewID
		pr.LastReviewCommentID = existing.LastReviewCommentID
		pr.ChecksHeadSHA = existing.ChecksHeadSHA
		pr.ChecksState = existing.ChecksState
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
		HeadSHA:        ghPR.GetHead().GetSHA(),
		Title:          ghPR.GetTitle(),
		State:          state,
		IsAssignedToMe: assigned,
		HTMLURL:        ghPR.GetHTMLURL(),
		AuthorLogin:    ghPR.GetUser().GetLogin(),
	}
}

func (s *GitService) SyncBranches(ctx context.Context, repoID uint, repoPath string) error {
	if _, err := os.Stat(repoPath); err != nil {
		return fmt.Errorf("repo path unavailable, skipping branch sync: %s: %w", repoPath, err)
	}
	branches := s.branchStore.ListByRepo(repoID)
	for i := range branches {
		if err := s.SyncBranch(ctx, &branches[i], repoPath); err != nil {
			slog.Warn("failed to sync branch", "branch", branches[i].Name, "error", err)
		}
	}
	return nil
}
