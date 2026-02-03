package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/workspace"
)

var apiClient = &http.Client{
	Timeout: 10 * time.Second,
}

const baseURL = "http://localhost:3333"

type sessionsLoadedMsg struct {
	sessions []session.Session
}

type workspacesLoadedMsg struct {
	workspaces []workspace.Workspace
}

type sessionActivatedMsg struct{}

type sessionCreatedMsg struct{}

func fetchSessions() tea.Cmd {
	return func() tea.Msg {
		res, err := apiClient.Get(baseURL + "/sessions")
		if err != nil {
			return errMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return errMsg{fmt.Errorf("fetch sessions: unexpected status %d", res.StatusCode)}
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
			return errMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return errMsg{fmt.Errorf("fetch workspaces: unexpected status %d", res.StatusCode)}
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
			return errMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return errMsg{fmt.Errorf("activate session: unexpected status %d", res.StatusCode)}
		}

		return sessionActivatedMsg{}
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
			return errMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
			return errMsg{fmt.Errorf("create session: unexpected status %d", res.StatusCode)}
		}

		return sessionCreatedMsg{}
	}
}
