package tui

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

var apiClient = &http.Client{
	Timeout: 10 * time.Second,
}

var baseURL string
var pipeName string

func Configure(port, pipe string) {
	baseURL = fmt.Sprintf("http://localhost:%s", port)
	pipeName = pipe
}

func parseAPIError(res *http.Response, fallback string) errMsg {
	body, _ := io.ReadAll(res.Body)
	log.Printf("[ERROR] %s: status=%d body=%s", fallback, res.StatusCode, string(body))

	var errResp struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
		return errMsg{errors.New(errResp.Error)}
	}
	return errMsg{fmt.Errorf("%s: unexpected status %d", fallback, res.StatusCode)}
}

type sessionsLoadedMsg struct {
	sessions []session.Session
}

type workspacesLoadedMsg struct {
	workspaces []workspace.Workspace
}

type sessionActivatedMsg struct {
	name          string
	pipeCommand   string
	workspacePath string
}

type sessionCreatedMsg struct {
	worktreePath string
}

type claudeSessionsLoadedMsg struct {
	claudeSessions []claude.ClaudeSession
}

type pipeSentMsg struct{}

type todosLoadedMsg struct {
	todos []todo.Todo
}

type sessionDeletedMsg struct {
	id string
}

type workspaceAddedMsg struct{}

type todoCreatedMsg struct{}

type todoDeletedMsg struct {
	id string
}

type branchesLoadedMsg struct {
	branches []string
}

type errMsg struct{ err error }

func fetchSessions() tea.Cmd {
	return func() tea.Msg {
		res, err := apiClient.Get(baseURL + "/sessions")
		if err != nil {
			log.Printf("[ERROR] fetch sessions: %v", err)
			return errMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "fetch sessions")
		}

		var resp session.SessionListResponse
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			return errMsg{err}
		}

		return sessionsLoadedMsg{sessions: resp.Sessions}
	}
}

func fetchClaudeSessions() tea.Cmd {
	return func() tea.Msg {
		res, err := apiClient.Get(baseURL + "/claude/sessions")
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

func fetchWorkspaces() tea.Cmd {
	return func() tea.Msg {
		res, err := apiClient.Get(baseURL + "/workspaces")
		if err != nil {
			log.Printf("[ERROR] fetch workspaces: %v", err)
			return errMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "fetch workspaces")
		}

		var resp workspace.WorkspaceListResponse
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			return errMsg{err}
		}

		return workspacesLoadedMsg{workspaces: resp.Workspaces}
	}
}

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

func activateSession(name string) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodPut, baseURL+"/sessions/"+name+"/activate", nil)
		if err != nil {
			return errMsg{err}
		}

		res, err := apiClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] activate session %q: %v", name, err)
			return errMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "activate session")
		}

		return sessionActivatedMsg{name: name}
	}
}

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

func deleteSession(id string) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodDelete, baseURL+"/sessions/"+id, nil)
		if err != nil {
			return errMsg{err}
		}

		res, err := apiClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] delete session %q: %v", id, err)
			return errMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusNoContent {
			return parseAPIError(res, "delete session")
		}

		return sessionDeletedMsg{id: id}
	}
}

func addWorkspace(path string, asRoot bool) tea.Cmd {
	return func() tea.Msg {
		body := map[string]interface{}{
			"path":    path,
			"as_root": asRoot,
		}
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return errMsg{err}
		}

		res, err := apiClient.Post(baseURL+"/workspaces", "application/json", bytes.NewReader(jsonBody))
		if err != nil {
			log.Printf("[ERROR] add workspace %q: %v", path, err)
			return errMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusCreated {
			return parseAPIError(res, "add workspace")
		}

		return workspaceAddedMsg{}
	}
}

func fetchTodos() tea.Cmd {
	return func() tea.Msg {
		res, err := apiClient.Get(baseURL + "/todos")
		if err != nil {
			log.Printf("[ERROR] fetch todos: %v", err)
			return errMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "fetch todos")
		}

		var resp todo.TodoListResponse
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			return errMsg{err}
		}

		return todosLoadedMsg{todos: resp.Todos}
	}
}

func createTodo(name, description, workspaceID string) tea.Cmd {
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
			return errMsg{err}
		}

		res, err := apiClient.Post(baseURL+"/todos", "application/json", bytes.NewReader(jsonBody))
		if err != nil {
			log.Printf("[ERROR] create todo: %v", err)
			return errMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusCreated {
			return parseAPIError(res, "create todo")
		}

		return todoCreatedMsg{}
	}
}

func deleteTodo(id string) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodDelete, baseURL+"/todos/"+id, nil)
		if err != nil {
			return errMsg{err}
		}

		res, err := apiClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] delete todo %q: %v", id, err)
			return errMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusNoContent {
			return parseAPIError(res, "delete todo")
		}

		return todoDeletedMsg{id: id}
	}
}

func sendZellijPipe(command, sessionName, workspacePath string) tea.Cmd {
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
			return pipeSentMsg{}
		}

		log.Printf("[INFO] sending zellij pipe: %s", string(jsonPayload))

		cmd := exec.Command("zellij", "pipe", "--name", pipeName)
		cmd.Stdin = strings.NewReader(string(jsonPayload))
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("[ERROR] zellij pipe failed: %v output: %s", err, string(output))
		}

		return pipeSentMsg{}
	}
}
