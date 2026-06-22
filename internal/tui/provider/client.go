package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/GianlucaP106/gotmux/gotmux"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/todo"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type client struct {
	httpClient *http.Client
	baseURL    string
}

func newClient(baseURL string) *client {
	return &client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		baseURL:    baseURL,
	}
}

func parseAPIError(res *http.Response, fallback string) ErrMsg {
	body, _ := io.ReadAll(res.Body)
	log.Printf("[ERROR] %s: status=%d body=%s", fallback, res.StatusCode, string(body))

	var errResp struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
		return ErrMsg{errors.New(errResp.Error)}
	}
	return ErrMsg{fmt.Errorf("%s: unexpected status %d", fallback, res.StatusCode)}
}

func (c *client) fetchSessions() tea.Cmd {
	return func() tea.Msg {
		res, err := c.httpClient.Get(c.baseURL + "/sessions")
		if err != nil {
			log.Printf("[ERROR] fetch sessions: %v", err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "fetch sessions")
		}

		var resp session.SessionListResponse
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			return ErrMsg{err}
		}

		sessions := make([]session.Session, len(resp.Sessions))
		for i, sr := range resp.Sessions {
			sessions[i] = *sr.Session
		}
		return sessionsLoadedMsg{sessions}
	}
}

func (c *client) fetchWorkspaces() tea.Cmd {
	return func() tea.Msg {
		res, err := c.httpClient.Get(c.baseURL + "/workspaces")
		if err != nil {
			log.Printf("[ERROR] fetch workspaces: %v", err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "fetch workspaces")
		}

		var resp workspace.WorkspaceListResponse
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			return ErrMsg{err}
		}

		return workspacesLoadedMsg{workspaces: resp.Workspaces}
	}
}

type branchListResponse struct {
	Branches      []string `json:"branches"`
	CurrentBranch string   `json:"current_branch"`
}

type branchRefListResponse struct {
	Branches      []git.BranchRef `json:"branches"`
	CurrentBranch string          `json:"current_branch"`
}

type branchExistsResponse struct {
	ExistsLocal  bool `json:"exists_local"`
	ExistsRemote bool `json:"exists_remote"`
}

func (c *client) fetchBranches(workspaceID uint) tea.Cmd {
	return func() tea.Msg {
		res, err := c.httpClient.Get(fmt.Sprintf("%s/workspaces/%d/branches", c.baseURL, workspaceID))
		if err != nil {
			log.Printf("[ERROR] fetch branches: %v", err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "fetch branches")
		}

		var resp branchListResponse
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			return ErrMsg{err}
		}

		return branchesLoadedMsg{branches: resp.Branches}
	}
}

func (c *client) fetchOriginBranches(workspaceID uint) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/workspaces/%d/branches/fetch", c.baseURL, workspaceID), nil)
		if err != nil {
			return branchesFetchedLoadedMsg{err: err}
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] fetch origin branches: %v", err)
			return branchesFetchedLoadedMsg{err: err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			apiErr := parseAPIError(res, "fetch origin branches")
			return branchesFetchedLoadedMsg{err: apiErr.Err}
		}

		var resp branchRefListResponse
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			return branchesFetchedLoadedMsg{err: err}
		}

		return branchesFetchedLoadedMsg{branches: resp.Branches}
	}
}

func (c *client) checkBranchExists(workspaceID uint, name string) tea.Cmd {
	return func() tea.Msg {
		u := fmt.Sprintf("%s/workspaces/%d/branches/exists?name=%s", c.baseURL, workspaceID, url.QueryEscape(name))
		res, err := c.httpClient.Get(u)
		if err != nil {
			log.Printf("[ERROR] check branch exists %q: %v", name, err)
			return branchExistsCheckedLoadedMsg{name: name, err: err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			apiErr := parseAPIError(res, "check branch exists")
			return branchExistsCheckedLoadedMsg{name: name, err: apiErr.Err}
		}

		var resp branchExistsResponse
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			return branchExistsCheckedLoadedMsg{name: name, err: err}
		}

		return branchExistsCheckedLoadedMsg{
			name:         name,
			existsLocal:  resp.ExistsLocal,
			existsRemote: resp.ExistsRemote,
		}
	}
}

func (c *client) fetchPRs(workspaceID uint, state string) tea.Cmd {
	return func() tea.Msg {
		url := fmt.Sprintf("%s/workspaces/%d/prs", c.baseURL, workspaceID)
		if state != "" {
			url += "?state=" + state
		}
		res, err := c.httpClient.Get(url)
		if err != nil {
			log.Printf("[ERROR] fetch PRs: %v", err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "fetch PRs")
		}

		var resp struct {
			PullRequests []git.PullRequest `json:"pull_requests"`
		}
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			return ErrMsg{err}
		}

		return prsLoadedMsg{prs: resp.PullRequests, workspaceID: workspaceID}
	}
}

func (c *client) activateSession(id uint) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/sessions/%d/activate", c.baseURL, id), nil)
		if err != nil {
			return ErrMsg{err}
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] activate session %d: %v", id, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "activate session")
		}

		var resp struct {
			TmuxSession *struct {
				Name string `json:"name"`
			} `json:"tmux_session"`
		}
		json.NewDecoder(res.Body).Decode(&resp)

		tmuxName := ""
		if resp.TmuxSession != nil {
			tmuxName = resp.TmuxSession.Name
		}
		return sessionActivatedMsg{tmuxSessionName: tmuxName}
	}
}

func (c *client) repairSession(id uint) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/sessions/%d/repair", c.baseURL, id), nil)
		if err != nil {
			return ErrMsg{err}
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] repair session %d: %v", id, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "repair session")
		}

		return sessionRepairedMsg{id: id}
	}
}

func (c *client) createSession(name string, specs []SessionWorkspaceSpec, todoID *uint) tea.Cmd {
	return func() tea.Msg {
		wss := make([]map[string]any, 0, len(specs))
		for _, s := range specs {
			item := map[string]any{"workspace_id": s.WorkspaceID}
			if s.Branch != "" {
				item["branch"] = s.Branch
			}
			if s.BaseBranch != "" {
				item["base_branch"] = s.BaseBranch
			}
			wss = append(wss, item)
		}
		body := map[string]interface{}{
			"workspaces": wss,
		}
		if name != "" {
			body["name"] = name
		}
		if todoID != nil {
			body["todo_id"] = *todoID
		}
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return ErrMsg{err}
		}

		res, err := c.httpClient.Post(c.baseURL+"/sessions", "application/json", bytes.NewReader(jsonBody))
		if err != nil {
			log.Printf("[ERROR] create session %q: %v", name, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusAccepted {
			return parseAPIError(res, "create session")
		}

		var resp struct {
			ID     uint   `json:"id"`
			Status string `json:"status"`
		}
		json.NewDecoder(res.Body).Decode(&resp)

		return SessionCreatedMsg{ID: resp.ID, Status: session.SessionStatus(resp.Status)}
	}
}

func (c *client) deleteSession(id uint, force bool) tea.Cmd {
	return func() tea.Msg {
		url := fmt.Sprintf("%s/sessions/%d", c.baseURL, id)
		if force {
			url += "?force=true"
		}
		req, err := http.NewRequest(http.MethodDelete, url, nil)
		if err != nil {
			return ErrMsg{err}
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] delete session %d: %v", id, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusNoContent {
			return parseAPIError(res, "delete session")
		}

		return sessionDeletedMsg{id: id}
	}
}

func (c *client) archiveSession(id uint) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/sessions/%d/archive", c.baseURL, id), nil)
		if err != nil {
			return ErrMsg{err}
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] archive session %d: %v", id, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "archive session")
		}

		return sessionArchivedMsg{id: id}
	}
}

func (c *client) dismissSession(id uint) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/sessions/%d/dismiss", c.baseURL, id), nil)
		if err != nil {
			return ErrMsg{err}
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] dismiss session %d: %v", id, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "dismiss session")
		}

		return sessionDismissedMsg{id: id}
	}
}

func (c *client) deleteWorkspace(id uint) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/workspaces/%d", c.baseURL, id), nil)
		if err != nil {
			return ErrMsg{err}
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] delete workspace %d: %v", id, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusNoContent {
			return parseAPIError(res, "delete workspace")
		}

		return workspaceDeletedMsg{}
	}
}

func (c *client) migrateWorkspaceToBare(id uint) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/workspaces/%d/migrate-bare", c.baseURL, id), nil)
		if err != nil {
			return workspaceMigrateStartedMsg{err: err}
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] migrate workspace %d to bare: %v", id, err)
			return workspaceMigrateStartedMsg{err: err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusAccepted {
			apiErr := parseAPIError(res, "migrate workspace to bare")
			return workspaceMigrateStartedMsg{err: apiErr.Err}
		}
		return workspaceMigrateStartedMsg{}
	}
}

func (c *client) setWorkspaceHidden(id uint, hidden bool) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]bool{"hidden": hidden})
		req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/workspaces/%d/hidden", c.baseURL, id), bytes.NewReader(body))
		if err != nil {
			return ErrMsg{err}
		}
		req.Header.Set("Content-Type", "application/json")

		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] set workspace hidden %d: %v", id, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusNoContent {
			return parseAPIError(res, "set workspace hidden")
		}

		return workspaceHiddenToggledMsg{}
	}
}

func (c *client) addWorkspace(path string, asRoot bool) tea.Cmd {
	return func() tea.Msg {
		body := map[string]interface{}{
			"path":    path,
			"as_root": asRoot,
		}
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return ErrMsg{err}
		}

		res, err := c.httpClient.Post(c.baseURL+"/workspaces", "application/json", bytes.NewReader(jsonBody))
		if err != nil {
			log.Printf("[ERROR] add workspace %q: %v", path, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusCreated {
			return parseAPIError(res, "add workspace")
		}

		return workspaceAddedMsg{}
	}
}

func (c *client) cloneWorkspace(cloneURL, rootPath, dirName string) tea.Cmd {
	return func() tea.Msg {
		body := map[string]string{"clone_url": cloneURL}
		if rootPath != "" {
			body["root_path"] = rootPath
		}
		if dirName != "" {
			body["dir_name"] = dirName
		}
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return workspaceClonedMsg{err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/workspaces/clone", bytes.NewReader(jsonBody))
		if err != nil {
			return workspaceClonedMsg{err: err}
		}
		req.Header.Set("Content-Type", "application/json")

		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] clone workspace %q: %v", cloneURL, err)
			return workspaceClonedMsg{err: err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusCreated {
			apiErr := parseAPIError(res, "clone workspace")
			return workspaceClonedMsg{err: apiErr.Err}
		}

		var ws workspace.Workspace
		if err := json.NewDecoder(res.Body).Decode(&ws); err != nil {
			return workspaceClonedMsg{err: err}
		}
		return workspaceClonedMsg{workspace: ws}
	}
}

func (c *client) fetchWorkspaceRoots() tea.Cmd {
	return func() tea.Msg {
		res, err := c.httpClient.Get(c.baseURL + "/workspaces/roots")
		if err != nil {
			log.Printf("[ERROR] fetch workspace roots: %v", err)
			return workspaceRootsMsg{err: err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			apiErr := parseAPIError(res, "fetch workspace roots")
			return workspaceRootsMsg{err: apiErr.Err}
		}

		var resp workspace.WorkspaceRootsResponse
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			return workspaceRootsMsg{err: err}
		}
		return workspaceRootsMsg{roots: resp.Roots}
	}
}

func (c *client) fetchTodos() tea.Cmd {
	return func() tea.Msg {
		res, err := c.httpClient.Get(c.baseURL + "/todos")
		if err != nil {
			log.Printf("[ERROR] fetch todos: %v", err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "fetch todos")
		}

		var resp todo.TodoListResponse
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			return ErrMsg{err}
		}

		todos := make([]todo.Todo, len(resp.Todos))
		for i, tr := range resp.Todos {
			todos[i] = *tr.Todo
		}
		return todosLoadedMsg{todos}
	}
}

func (c *client) createTodo(name, description string, workspaceID *uint) tea.Cmd {
	return func() tea.Msg {
		body := map[string]interface{}{
			"name":        name,
			"description": description,
		}
		if workspaceID != nil {
			body["workspace_id"] = *workspaceID
		}
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return ErrMsg{err}
		}

		res, err := c.httpClient.Post(c.baseURL+"/todos", "application/json", bytes.NewReader(jsonBody))
		if err != nil {
			log.Printf("[ERROR] create todo: %v", err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusCreated {
			return parseAPIError(res, "create todo")
		}

		return TodoCreatedMsg{}
	}
}

func (c *client) deleteTodo(id uint) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/todos/%d", c.baseURL, id), nil)
		if err != nil {
			return ErrMsg{err}
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] delete todo %d: %v", id, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusNoContent {
			return parseAPIError(res, "delete todo")
		}

		return todoDeletedMsg{id: id}
	}
}

func (c *client) addWorkspaceToSession(sessionID uint, spec SessionWorkspaceSpec) tea.Cmd {
	return func() tea.Msg {
		item := map[string]any{"workspace_id": spec.WorkspaceID}
		if spec.Branch != "" {
			item["branch"] = spec.Branch
		}
		if spec.BaseBranch != "" {
			item["base_branch"] = spec.BaseBranch
		}
		jsonBody, err := json.Marshal(item)
		if err != nil {
			return ErrMsg{err}
		}

		res, err := c.httpClient.Post(
			fmt.Sprintf("%s/sessions/%d/workspaces", c.baseURL, sessionID),
			"application/json",
			bytes.NewReader(jsonBody),
		)
		if err != nil {
			log.Printf("[ERROR] add workspace to session %d: %v", sessionID, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusAccepted {
			return parseAPIError(res, "add workspace to session")
		}

		var resp struct {
			ID uint `json:"id"`
		}
		json.NewDecoder(res.Body).Decode(&resp)
		return workspaceAddedToSessionMsg{sessionID: resp.ID}
	}
}

func (c *client) getSession(id uint) tea.Cmd {
	return func() tea.Msg {
		res, err := c.httpClient.Get(fmt.Sprintf("%s/sessions/%d", c.baseURL, id))
		if err != nil {
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "get session")
		}

		var resp session.SessionResponse
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			return ErrMsg{err}
		}

		return sessionPolledMsg{session: *resp.Session}
	}
}

func (c *client) switchTmuxSession(name string) tea.Cmd {
	return func() tea.Msg {
		t, err := gotmux.DefaultTmux()
		if err != nil {
			log.Printf("[ERROR] gotmux init failed: %v", err)
			return ErrMsg{err}
		}
		if err := t.SwitchClient(&gotmux.SwitchClientOptions{TargetSession: name}); err != nil {
			log.Printf("[ERROR] tmux switch-client failed: %v", err)
		}
		return SessionSwitchedMsg{}
	}
}
