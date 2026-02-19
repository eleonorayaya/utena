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
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/workspace"
)

var apiClient = &http.Client{
	Timeout: 10 * time.Second,
}

const baseURL = "http://localhost:3333"

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
	name string
}

type sessionCreatedMsg struct{}

type pipeSentMsg struct{}

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

func createSession(name, workspaceID string) tea.Cmd {
	return func() tea.Msg {
		body := map[string]string{
			"id":           name,
			"workspace_id": workspaceID,
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

		return sessionCreatedMsg{}
	}
}

type workspaceAddedMsg struct{}

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

		cmd := exec.Command("zellij", "pipe", "--name", "utena-commands")
		cmd.Stdin = strings.NewReader(string(jsonPayload))
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("[ERROR] zellij pipe failed: %v output: %s", err, string(output))
		}

		return pipeSentMsg{}
	}
}
