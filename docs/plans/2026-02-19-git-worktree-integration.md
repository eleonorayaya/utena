# Git Worktree Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add git worktree support so that creating a session for a git-backed workspace prompts for a base branch, pulls latest, and creates a worktree at `.worktrees/<session-name>`.

**Architecture:** New `internal/git/` package with a stateless `GitService` wrapping `os/exec` git commands. The workspace controller exposes a `GET /{id}/branches` endpoint that delegates to `GitService`. The session service calls `GitService.Pull` and `GitService.CreateWorktree` during `CreateSession` when `BaseBranch` is set. A new `BranchPickerModel` TUI view is inserted between workspace picker and name input for git repos.

**Tech Stack:** Go, chi router, Bubbletea + bubbles/list, os/exec for git commands

**Design doc:** `docs/plans/2026-02-19-git-worktree-integration-design.md`

---

### Task 1: Create GitService

**Files:**
- Create: `internal/git/gitservice.go`

**Step 1: Create the git service file**

```go
package git

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type GitService struct{}

func NewGitService() *GitService {
	return &GitService{}
}

func (s *GitService) ListBranches(ctx context.Context, repoPath string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "branch", "--format=%(refname:short)")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var branches []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			branches = append(branches, trimmed)
		}
	}
	return branches, nil
}

func (s *GitService) Pull(ctx context.Context, repoPath string, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "pull", "origin", branch)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git pull failed: %s: %w", string(output), err)
	}
	return nil
}

func (s *GitService) CreateWorktree(ctx context.Context, repoPath string, name string, baseBranch string) (string, error) {
	worktreePath := filepath.Join(repoPath, ".worktrees", name)
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "worktree", "add", "-b", name, worktreePath, baseBranch)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add failed: %s: %w", string(output), err)
	}
	return worktreePath, nil
}
```

**Step 2: Verify it compiles**

Run: `task test`
Expected: All existing tests pass (no code depends on the new package yet).

**Step 3: Commit**

```bash
git add internal/git/gitservice.go
git commit -m "feat: add git service for branch listing, pull, and worktree creation"
```

---

### Task 2: Add branches endpoint to workspace module

**Files:**
- Modify: `internal/workspace/types.go:44-55` (add response type)
- Modify: `internal/workspace/workspacecontroller.go:1-19` (add gitService dependency and ListBranches handler)
- Modify: `internal/workspace/workspacerouter.go:17-25` (add route)
- Modify: `internal/workspace/workspacemodule.go:1-29` (create and export GitService)

**Step 1: Add BranchListResponse to types.go**

Append after the `AddWorkspaceRequest` type at the end of `internal/workspace/types.go`:

```go
type BranchListResponse struct {
	Branches []string `json:"branches"`
}
```

**Step 2: Add gitService to WorkspaceController**

In `internal/workspace/workspacecontroller.go`, change the struct and constructor:

```go
import (
	"fmt"
	"net/http"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

type WorkspaceController struct {
	service    *WorkspaceService
	gitService *git.GitService
}

func NewWorkspaceController(service *WorkspaceService, gitService *git.GitService) *WorkspaceController {
	return &WorkspaceController{
		service:    service,
		gitService: gitService,
	}
}
```

Add the `ListBranches` handler at the end of the file:

```go
func (c *WorkspaceController) ListBranches(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	ws, err := c.service.GetWorkspace(ctx, id)
	if err != nil {
		render.Render(w, r, common.ErrNotFound())
		return
	}

	if !ws.IsGitRepo {
		render.Render(w, r, common.ErrInvalidRequest(fmt.Errorf("workspace is not a git repository")))
		return
	}

	branches, err := c.gitService.ListBranches(ctx, ws.Path)
	if err != nil {
		render.Render(w, r, common.ErrUnknown(err))
		return
	}

	render.JSON(w, r, BranchListResponse{Branches: branches})
}
```

**Step 3: Add route to workspace router**

In `internal/workspace/workspacerouter.go`, add the route BEFORE `/{id}` (chi matches top-to-bottom, `/{id}/branches` is more specific):

```go
func (wr *WorkspaceRouter) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", wr.controller.ListWorkspaces)
	r.Post("/", wr.controller.AddWorkspace)
	r.Get("/{id}/branches", wr.controller.ListBranches)
	r.Get("/{id}", wr.controller.GetWorkspaceByID)

	return r
}
```

**Step 4: Wire GitService in workspace module**

In `internal/workspace/workspacemodule.go`:

```go
import (
	"context"

	"github.com/eleonorayaya/utena/internal/git"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/afero"
)

type WorkspaceModule struct {
	Store      *WorkspaceStore
	Service    *WorkspaceService
	Controller *WorkspaceController
	Router     *WorkspaceRouter
	GitService *git.GitService
}

func NewWorkspaceModule(fs afero.Fs, configDir string) *WorkspaceModule {
	store := NewWorkspaceStore(fs, configDir)
	service := NewWorkspaceService(store)
	gitService := git.NewGitService()
	controller := NewWorkspaceController(service, gitService)
	router := NewWorkspaceRouter(controller)

	return &WorkspaceModule{
		Store:      store,
		Service:    service,
		Controller: controller,
		Router:     router,
		GitService: gitService,
	}
}
```

The rest of `workspacemodule.go` (OnAppStart, OnAppEnd, Routes) stays unchanged.

**Step 5: Fix workspace router test**

In `internal/workspace/workspacerouter_test.go`, update `setupWorkspaceRouter` (line 24-26) and the AddWorkspace test (line 97):

In `setupWorkspaceRouter`:
```go
	service := NewWorkspaceService(store)
	gitService := git.NewGitService()
	controller := NewWorkspaceController(service, gitService)
```

Add the import: `"github.com/eleonorayaya/utena/internal/git"`

In the `TestWorkspaceRouter_AddWorkspace` test, same change:
```go
	service := NewWorkspaceService(store)
	gitService := git.NewGitService()
	controller := NewWorkspaceController(service, gitService)
```

**Step 6: Verify tests pass**

Run: `task test`
Expected: All tests pass.

**Step 7: Commit**

```bash
git add internal/workspace/types.go internal/workspace/workspacecontroller.go internal/workspace/workspacerouter.go internal/workspace/workspacemodule.go internal/workspace/workspacerouter_test.go
git commit -m "feat: add GET /workspaces/{id}/branches endpoint"
```

---

### Task 3: Add worktree creation to session service

**Files:**
- Modify: `internal/session/session.go:11-19` (add fields)
- Modify: `internal/session/sessionservice.go:1-24,68-86` (add gitService dep, worktree logic)
- Modify: `internal/session/sessionmodule.go:19-21` (pass GitService)

**Step 1: Add fields to Session model**

In `internal/session/session.go`, add `BaseBranch` and `WorktreePath` after `WorkspaceName`:

```go
type Session struct {
	ID            string    `json:"id"`
	WorkspaceID   string    `json:"workspace_id"`
	WorkspaceName string    `json:"workspace_name,omitempty"`
	BaseBranch    string    `json:"base_branch,omitempty"`
	WorktreePath  string    `json:"worktree_path,omitempty"`
	IsAttached    bool      `json:"is_attached"`
	IsActive      bool      `json:"is_active"`
	IsDead        bool      `json:"is_dead"`
	LastUsedAt    time.Time `json:"last_used_at"`
}
```

**Step 2: Add gitService to SessionService**

In `internal/session/sessionservice.go`, update the struct, constructor, and CreateSession:

```go
import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type SessionService struct {
	store          *SessionStore
	workspaceStore *workspace.WorkspaceStore
	gitService     *git.GitService
	eventBus       eventbus.EventBus
}

func NewSessionService(store *SessionStore, workspaceStore *workspace.WorkspaceStore, gitService *git.GitService, bus eventbus.EventBus) *SessionService {
	return &SessionService{
		store:          store,
		workspaceStore: workspaceStore,
		gitService:     gitService,
		eventBus:       bus,
	}
}
```

Update `CreateSession`:

```go
func (s *SessionService) CreateSession(ctx context.Context, session *Session) error {
	var ws *workspace.Workspace
	if session.WorkspaceID != "" {
		var err error
		ws, err = s.workspaceStore.GetByID(session.WorkspaceID)
		if err != nil {
			return err
		}
	}

	if session.BaseBranch != "" && ws != nil && ws.IsGitRepo {
		if err := s.gitService.Pull(ctx, ws.Path, session.BaseBranch); err != nil {
			slog.Warn("git pull failed, continuing with worktree creation", "error", err)
		}

		worktreePath, err := s.gitService.CreateWorktree(ctx, ws.Path, session.ID, session.BaseBranch)
		if err != nil {
			return fmt.Errorf("failed to create worktree: %w", err)
		}
		session.WorktreePath = worktreePath
	}

	if session.LastUsedAt.IsZero() {
		session.LastUsedAt = time.Now()
	}

	if err := s.store.Add(session); err != nil {
		return err
	}

	return nil
}
```

**Step 3: Wire GitService in session module**

In `internal/session/sessionmodule.go`, line 21, change:

```go
service := NewSessionService(store, workspaceModule.Store, workspaceModule.GitService, bus)
```

**Step 4: Fix all test files that call NewSessionService**

In `internal/session/sessionservice_test.go`, update `setupSessionService` (line 24):
```go
	gitService := git.NewGitService()
	service := NewSessionService(sessionStore, workspaceStore, gitService, bus)
```
Add import: `"github.com/eleonorayaya/utena/internal/git"`

In `internal/session/sessionrouter_test.go`, update `setupSessionRouter` (line 27):
```go
	gitService := git.NewGitService()
	service := NewSessionService(sessionStore, workspaceStore, gitService, bus)
```
Add import: `"github.com/eleonorayaya/utena/internal/git"`

In `internal/zellij/zellijservice_test.go`, update `setupZellijService` (line 26):
```go
	gitService := git.NewGitService()
	sessionService := session.NewSessionService(sessionStore, workspaceStore, gitService, bus)
```
Add import: `"github.com/eleonorayaya/utena/internal/git"`

**Step 5: Verify tests pass**

Run: `task test`
Expected: All tests pass. Existing tests don't set `BaseBranch`, so no worktree logic triggers.

**Step 6: Commit**

```bash
git add internal/session/session.go internal/session/sessionservice.go internal/session/sessionmodule.go internal/session/sessionservice_test.go internal/session/sessionrouter_test.go internal/zellij/zellijservice_test.go
git commit -m "feat: add worktree creation to session service"
```

---

### Task 4: Create branch picker TUI view

**Files:**
- Create: `internal/tui/branchpicker.go`

**Step 1: Create the branch picker model**

```go
package tui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type switchToBranchPickerMsg struct {
	workspace workspace.Workspace
}

type branchSelectedMsg struct {
	workspace workspace.Workspace
	branch    string
}

type branchItem struct {
	name string
}

func (i branchItem) Title() string       { return i.name }
func (i branchItem) Description() string { return "" }
func (i branchItem) FilterValue() string { return i.name }

type BranchPickerModel struct {
	list      list.Model
	workspace workspace.Workspace
}

func NewBranchPickerModel(ws workspace.Workspace) BranchPickerModel {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Select base branch"
	l.KeyMap.Quit.SetEnabled(false)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{selectKey, backKey}
	}
	return BranchPickerModel{list: l, workspace: ws}
}

func (m *BranchPickerModel) SetSize(width, height int) {
	m.list.SetWidth(width)
	m.list.SetHeight(height)
}

func (m BranchPickerModel) Init() tea.Cmd {
	return nil
}

type branchesLoadedMsg struct {
	branches []string
}

func (m BranchPickerModel) Update(msg tea.Msg) (BranchPickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case branchesLoadedMsg:
		items := make([]list.Item, len(msg.branches))
		for i, b := range msg.branches {
			items[i] = branchItem{name: b}
		}
		cmd := m.list.SetItems(items)
		return m, cmd

	case tea.KeyMsg:
		if m.list.FilterState() == list.Filtering {
			break
		}
		switch {
		case key.Matches(msg, selectKey):
			if item, ok := m.list.SelectedItem().(branchItem); ok {
				ws := m.workspace
				branch := item.name
				return m, func() tea.Msg {
					return branchSelectedMsg{workspace: ws, branch: branch}
				}
			}
		case key.Matches(msg, backKey):
			return m, func() tea.Msg {
				return switchToNewSessionMsg{}
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m BranchPickerModel) View() string {
	return m.list.View()
}
```

**Step 2: Verify it compiles**

Run: `task test`
Expected: All tests pass.

**Step 3: Commit**

```bash
git add internal/tui/branchpicker.go
git commit -m "feat: add branch picker TUI view"
```

---

### Task 5: Add fetchBranches client function and update createSession

**Files:**
- Modify: `internal/tui/client.go:39-51,121-145` (add fetchBranches, update createSession and sessionCreatedMsg)

**Step 1: Update sessionCreatedMsg**

In `internal/tui/client.go`, replace the empty `sessionCreatedMsg` (line 51):

```go
type sessionCreatedMsg struct {
	worktreePath string
}
```

**Step 2: Add fetchBranches function**

Add after the `fetchWorkspaces` function (after line 97):

```go
type branchListResponse struct {
	Branches []string `json:"branches"`
}

func fetchBranches(workspaceID string) tea.Cmd {
	return func() tea.Msg {
		res, err := apiClient.Get(baseURL + "/workspaces/" + workspaceID + "/branches")
		if err != nil {
			log.Printf("[ERROR] fetch branches: %v", err)
			return errMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "fetch branches")
		}

		var resp branchListResponse
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			return errMsg{err}
		}

		return branchesLoadedMsg{branches: resp.Branches}
	}
}
```

**Step 3: Update createSession to accept baseBranch and return worktreePath**

Replace the `createSession` function (lines 121-145):

```go
func createSession(name, workspaceID, baseBranch string) tea.Cmd {
	return func() tea.Msg {
		body := map[string]string{
			"id":           name,
			"workspace_id": workspaceID,
		}
		if baseBranch != "" {
			body["base_branch"] = baseBranch
		}
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return errMsg{err}
		}

		res, err := apiClient.Post(baseURL+"/sessions", "application/json", bytes.NewReader(jsonBody))
		if err != nil {
			log.Printf("[ERROR] create session %q: %v", name, err)
			return errMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
			return parseAPIError(res, "create session")
		}

		var resp struct {
			WorktreePath string `json:"worktree_path"`
		}
		json.NewDecoder(res.Body).Decode(&resp)

		return sessionCreatedMsg{worktreePath: resp.WorktreePath}
	}
}
```

**Step 4: Verify it compiles**

Run: `task test`
Expected: All tests pass.

**Step 5: Commit**

```bash
git add internal/tui/client.go
git commit -m "feat: add fetchBranches and update createSession for worktree support"
```

---

### Task 6: Wire branch picker into TUI app and update workspace picker

**Files:**
- Modify: `internal/tui/app.go:15-21,23-35,53-145,147-160` (add view, field, transitions, rendering)
- Modify: `internal/tui/newsession.go:65-70` (conditional routing for git repos)

**Step 1: Add branchPickerView to view enum**

In `internal/tui/app.go`, insert `branchPickerView` after `workspacePickerView`:

```go
const (
	sessionListView view = iota
	workspacePickerView
	branchPickerView
	nameInputView
	debugView
	filePickerView
)
```

**Step 2: Add branchPicker and pendingBranch to App struct**

```go
type App struct {
	activeView           view
	previousView         view
	sessionList          SessionListModel
	newSession           NewSessionModel
	branchPicker         BranchPickerModel
	nameInput            NameInputModel
	filePicker           FilePickerModel
	help                 help.Model
	pendingCreate        string
	pendingWorkspacePath string
	pendingBranch        string
	logPath              string
	width, height        int
}
```

**Step 3: Propagate WindowSizeMsg to branch picker**

In the `tea.WindowSizeMsg` case (around line 56-59), add:

```go
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.sessionList.SetSize(msg.Width, msg.Height)
		a.newSession.SetSize(msg.Width, msg.Height)
		a.branchPicker.SetSize(msg.Width, msg.Height)
```

**Step 4: Add branch picker transitions in Update**

Add these cases in the `Update` method's message switch (between `switchToNewSessionMsg` and `switchToNameInputMsg`):

Add the `switchToBranchPickerMsg` case:
```go
	case switchToBranchPickerMsg:
		a.activeView = branchPickerView
		a.branchPicker = NewBranchPickerModel(msg.workspace)
		a.branchPicker.SetSize(a.width, a.height)
		return a, fetchBranches(msg.workspace.ID)
```

Add the `branchSelectedMsg` case:
```go
	case branchSelectedMsg:
		a.activeView = nameInputView
		a.pendingBranch = msg.branch
		a.nameInput = NewNameInputModel(msg.workspace)
		return a, a.nameInput.Init()
```

**Step 5: Update switchToNameInputMsg to clear pendingBranch**

Change the existing `switchToNameInputMsg` case:

```go
	case switchToNameInputMsg:
		a.activeView = nameInputView
		a.pendingBranch = ""
		a.nameInput = NewNameInputModel(msg.workspace)
		return a, a.nameInput.Init()
```

**Step 6: Update createSessionMsg to pass pendingBranch**

Change the existing `createSessionMsg` case:

```go
	case createSessionMsg:
		a.pendingCreate = msg.name
		a.pendingWorkspacePath = a.nameInput.workspace.Path
		return a, createSession(msg.name, msg.workspaceID, a.pendingBranch)
```

**Step 7: Update sessionCreatedMsg to use worktree path**

Change the existing `sessionCreatedMsg` case:

```go
	case sessionCreatedMsg:
		if msg.worktreePath != "" {
			a.pendingWorkspacePath = msg.worktreePath
		}
		return a, activateSession(a.pendingCreate)
```

**Step 8: Add errMsg handling for branch picker**

Update the `errMsg` case to handle branchPickerView:

```go
	case errMsg:
		switch a.activeView {
		case branchPickerView:
			return a, a.branchPicker.list.NewStatusMessage(msg.err.Error())
		case nameInputView:
			a.nameInput.err = msg.err.Error()
			return a, nil
		case sessionListView:
			return a, a.sessionList.list.NewStatusMessage(msg.err.Error())
		}
		return a, nil
```

**Step 9: Add branchPickerView to the view dispatch in Update**

In the `switch a.activeView` block at the bottom of Update (around lines 134-143):

```go
	var cmd tea.Cmd
	switch a.activeView {
	case sessionListView:
		a.sessionList, cmd = a.sessionList.Update(msg)
	case workspacePickerView:
		a.newSession, cmd = a.newSession.Update(msg)
	case branchPickerView:
		a.branchPicker, cmd = a.branchPicker.Update(msg)
	case nameInputView:
		a.nameInput, cmd = a.nameInput.Update(msg)
	case filePickerView:
		a.filePicker, cmd = a.filePicker.Update(msg)
	}
	return a, cmd
```

**Step 10: Add branchPickerView to View()**

In the `View()` method, add a case for `branchPickerView`:

```go
func (a App) View() string {
	switch a.activeView {
	case debugView:
		return a.debugViewContent()
	case workspacePickerView:
		return a.newSession.View()
	case branchPickerView:
		return a.branchPicker.View()
	case nameInputView:
		return a.nameInput.View() + "\n\n" + a.help.View(nameInputKeyMap)
	case filePickerView:
		return a.filePicker.View()
	default:
		return a.sessionList.View()
	}
}
```

**Step 11: Update workspace picker to route git repos to branch picker**

In `internal/tui/newsession.go`, change the `selectKey` handler (lines 65-70):

```go
		case key.Matches(msg, selectKey):
			if item, ok := m.list.SelectedItem().(workspaceItem); ok {
				if item.workspace.IsGitRepo {
					return m, func() tea.Msg {
						return switchToBranchPickerMsg{workspace: item.workspace}
					}
				}
				return m, func() tea.Msg {
					return switchToNameInputMsg{workspace: item.workspace}
				}
			}
```

**Step 12: Verify it compiles**

Run: `task test`
Expected: All tests pass.

**Step 13: Commit**

```bash
git add internal/tui/app.go internal/tui/newsession.go
git commit -m "feat: wire branch picker into TUI session creation flow"
```

---

### Task 7: Manual integration test

**Step 1: Build and run the daemon**

Run: `task daemon:run`

**Step 2: Build and run the TUI (in another terminal)**

Run: `task tui:run`

**Step 3: Test the flow**

1. Press `n` to create a new session
2. Select a git-backed workspace → should show branch picker
3. Select a branch (e.g., `main`) → should show name input
4. Enter a session name → should create the session with a worktree
5. Verify worktree was created at `<workspace>/.worktrees/<session-name>`

**Step 4: Test non-git workspace**

1. Press `n` to create a new session
2. Select a non-git workspace → should skip branch picker and go straight to name input

**Step 5: Test the branches API directly**

Run: `curl http://localhost:3333/workspaces/<workspace-id>/branches`
Expected: `{"branches":["main","develop",...]}`

**Step 6: Final commit (if any fixes needed)**

```bash
git add -A
git commit -m "fix: integration test fixes for git worktree flow"
```
