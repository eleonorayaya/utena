package provider

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/todo"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type client struct {
	httpClient *http.Client
	baseURL    string
	pipeName   string
}

func newClient(baseURL, pipeName string) *client {
	return &client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		baseURL:    baseURL,
		pipeName:   pipeName,
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
	Branches []string `json:"branches"`
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

		return sessionActivatedMsg{name: name}
	}
}

func (c *client) reviveSession(name string) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodPut, c.baseURL+"/sessions/"+name+"/revive", nil)
		if err != nil {
			return ErrMsg{err}
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] revive session %q: %v", name, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "revive session")
		}

		var resp struct {
			WorkspacePath string `json:"workspace_path"`
			WorktreePath  string `json:"worktree_path"`
		}
		json.NewDecoder(res.Body).Decode(&resp)

		return sessionRevivedMsg{
			name:          name,
			workspacePath: resp.WorkspacePath,
			worktreePath:  resp.WorktreePath,
		}
	}
}

func (c *client) createSession(name, workspaceID, baseBranch string) tea.Cmd {
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
			return ErrMsg{err}
		}

		res, err := c.httpClient.Post(c.baseURL+"/sessions", "application/json", bytes.NewReader(jsonBody))
		if err != nil {
			log.Printf("[ERROR] create session %q: %v", name, err)
			return ErrMsg{err}
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

func (c *client) sendZellijPipe(command, sessionName, workspacePath string) tea.Cmd {
	return func() tea.Msg {
		payload := map[string]interface{}{
			"command": command,
		}
		if sessionName != "" {
			payload["session_name"] = sessionName
		}
		if workspacePath != "" {
			payload["workspace_path"] = workspacePath
		}

		jsonPayload, err := json.Marshal(payload)
		if err != nil {
			log.Printf("[ERROR] marshal pipe command: %v", err)
			return tea.Quit()
		}

		log.Printf("[INFO] sending zellij pipe: %s", string(jsonPayload))

		cmd := exec.Command("zellij", "pipe", "--name", c.pipeName)
		cmd.Stdin = strings.NewReader(string(jsonPayload))
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("[ERROR] zellij pipe failed: %v output: %s", err, string(output))
		}

		return tea.Quit()
	}
}
