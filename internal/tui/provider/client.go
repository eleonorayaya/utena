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
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tmux"
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

		return sessionsLoadedMsg{sessions: resp.Sessions}
	}
}

func (c *client) fetchClaudeSessions() tea.Cmd {
	return func() tea.Msg {
		res, err := c.httpClient.Get(c.baseURL + "/claude/sessions")
		if err != nil {
			log.Printf("[ERROR] fetch claude sessions: %v", err)
			return claudeSessionsLoadedMsg{}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			log.Printf("[ERROR] fetch claude sessions: status %d", res.StatusCode)
			return claudeSessionsLoadedMsg{}
		}

		var resp claude.ClaudeSessionListResponse
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			log.Printf("[ERROR] decode claude sessions: %v", err)
			return claudeSessionsLoadedMsg{}
		}

		return claudeSessionsLoadedMsg{claudeSessions: resp.ClaudeSessions}
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

func (c *client) fetchBranches(workspaceID string) tea.Cmd {
	return func() tea.Msg {
		res, err := c.httpClient.Get(c.baseURL + "/workspaces/" + workspaceID + "/branches")
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

func (c *client) activateSession(name string) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodPut, c.baseURL+"/sessions/"+name+"/activate", nil)
		if err != nil {
			return ErrMsg{err}
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] activate session %q: %v", name, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "activate session")
		}

		var resp struct {
			TmuxSessionName string `json:"tmux_session_name"`
		}
		json.NewDecoder(res.Body).Decode(&resp)

		return sessionActivatedMsg{tmuxSessionName: resp.TmuxSessionName}
	}
}

func (c *client) repairSession(id string) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodPut, c.baseURL+"/sessions/"+id+"/repair", nil)
		if err != nil {
			return ErrMsg{err}
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] repair session %q: %v", id, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "repair session")
		}

		return sessionRepairedMsg{id: id}
	}
}

func (c *client) createSession(name, workspaceID, branch string, branchCreated bool) tea.Cmd {
	return func() tea.Msg {
		body := map[string]interface{}{
			"workspace_id": workspaceID,
		}
		if name != "" {
			body["name"] = name
		}
		if branch != "" {
			body["create_worktree"] = true
			body["branch_created"] = branchCreated
			if branchCreated {
				body["base_branch"] = branch
			} else {
				body["branch"] = branch
			}
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
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		json.NewDecoder(res.Body).Decode(&resp)

		return sessionCreatedMsg{id: resp.ID, status: session.SessionStatus(resp.Status)}
	}
}

func (c *client) deleteSession(id string) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodDelete, c.baseURL+"/sessions/"+id, nil)
		if err != nil {
			return ErrMsg{err}
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] delete session %q: %v", id, err)
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

		return todosLoadedMsg{todos: resp.Todos}
	}
}

func (c *client) fetchWindows(sessionName string) tea.Cmd {
	return func() tea.Msg {
		res, err := c.httpClient.Get(c.baseURL + "/tmux/windows/" + sessionName)
		if err != nil {
			log.Printf("[ERROR] fetch windows: %v", err)
			return windowsLoadedMsg{}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			log.Printf("[ERROR] fetch windows: status %d", res.StatusCode)
			return windowsLoadedMsg{}
		}

		var windows []tmux.Window
		if err := json.NewDecoder(res.Body).Decode(&windows); err != nil {
			log.Printf("[ERROR] decode windows: %v", err)
			return windowsLoadedMsg{}
		}

		return windowsLoadedMsg{windows: windows}
	}
}

func (c *client) createTodo(name, description, workspaceID string) tea.Cmd {
	return func() tea.Msg {
		body := map[string]string{
			"name":        name,
			"description": description,
		}
		if workspaceID != "" {
			body["workspace_id"] = workspaceID
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

func (c *client) deleteTodo(id string) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodDelete, c.baseURL+"/todos/"+id, nil)
		if err != nil {
			return ErrMsg{err}
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] delete todo %q: %v", id, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusNoContent {
			return parseAPIError(res, "delete todo")
		}

		return todoDeletedMsg{id: id}
	}
}

func (c *client) switchTmuxSession(name string) tea.Cmd {
	return func() tea.Msg {
		t, err := gotmux.DefaultTmux()
		if err != nil {
			log.Printf("[ERROR] gotmux init failed: %v", err)
			return tea.Quit()
		}
		if err := t.SwitchClient(&gotmux.SwitchClientOptions{TargetSession: name}); err != nil {
			log.Printf("[ERROR] tmux switch-client failed: %v", err)
		}
		return tea.Quit()
	}
}
