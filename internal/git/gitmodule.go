package git

import (
	"context"
	"time"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	usync "github.com/eleonorayaya/utena/internal/sync"
	"github.com/go-chi/chi/v5"
)

type GitModule struct {
	Service *GitService
}

func NewGitModule(database db.Database, bus eventbus.EventBus) *GitModule {
	ghClient, _ := NewGitHubClient(context.Background())
	service := NewGitService(database, WithGitHubClient(ghClient), WithEventBus(bus))
	return &GitModule{Service: service}
}

func (m *GitModule) OnAppStart(ctx context.Context) error {
	return nil
}

func (m *GitModule) OnAppEnd(ctx context.Context) error {
	return nil
}

func (m *GitModule) Routes() chi.Router {
	r := chi.NewRouter()
	return r
}

func (m *GitModule) Models() []any {
	return []any{&Repo{}, &Branch{}, &Worktree{}, &PullRequest{}}
}

type prSyncTask struct {
	service *GitService
}

func (t *prSyncTask) Name() string           { return "git.prs" }
func (t *prSyncTask) Interval() time.Duration { return 5 * time.Minute }
func (t *prSyncTask) Run(ctx context.Context) error {
	repos := t.service.repoStore.List()
	for _, repo := range repos {
		if err := t.service.SyncRepoPRs(ctx, &repo); err != nil {
			continue
		}
	}
	_, _ = t.service.SyncAssignedPRs(ctx)
	return nil
}

type branchSyncTask struct {
	service *GitService
}

func (t *branchSyncTask) Name() string           { return "git.branches" }
func (t *branchSyncTask) Interval() time.Duration { return 2 * time.Minute }
func (t *branchSyncTask) Run(ctx context.Context) error {
	repos := t.service.repoStore.List()
	for _, repo := range repos {
		if err := t.service.SyncBranches(ctx, repo.ID, repo.Path); err != nil {
			continue
		}
	}
	return nil
}

func (m *GitModule) RegisterSyncTasks(manager *usync.SyncManager) {
	manager.Register(&prSyncTask{service: m.Service})
	manager.Register(&branchSyncTask{service: m.Service})
}
