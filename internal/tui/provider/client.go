package provider

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/GianlucaP106/gotmux/gotmux"
	tea "github.com/charmbracelet/bubbletea"
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

func (c *client) createSession(name string, workspaceID uint, branch string, baseBranch string, todoID *uint) tea.Cmd {
	return func() tea.Msg {
		body := map[string]interface{}{
			"workspace_id": workspaceID,
		}
		if name != "" {
			body["name"] = name
		}
		if branch != "" {
			body["branch"] = branch
		}
		if baseBranch != "" {
			body["base_branch"] = baseBranch
			body["create_worktree"] = true
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

func (c *client) deleteSession(id uint) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/sessions/%d", c.baseURL, id), nil)
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
