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
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/eleonorayaya/utena/internal/bundles"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/ui"
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
		statuses := make([]string, len(session.ListableStatuses))
		for i, s := range session.ListableStatuses {
			statuses[i] = string(s)
		}
		q := url.Values{"status": {strings.Join(statuses, ",")}}
		res, err := c.httpClient.Get(c.baseURL + "/sessions?" + q.Encode())
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

type branchExistsResponse struct {
	ExistsLocal  bool `json:"exists_local"`
	ExistsRemote bool `json:"exists_remote"`
}

func (c *client) fetchBranches(workspaceID uint) tea.Cmd {
	return func() tea.Msg {
		fail := func(err error) tea.Msg {
			return branchesLoadedMsg{BranchesLoadedMsg{WorkspaceID: workspaceID, Err: err}}
		}
		res, err := c.httpClient.Get(fmt.Sprintf("%s/workspaces/%d/branches", c.baseURL, workspaceID))
		if err != nil {
			log.Printf("[ERROR] fetch branches: %v", err)
			return fail(err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return fail(parseAPIError(res, "fetch branches").Err)
		}

		var resp workspace.BranchListResponse
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			return fail(err)
		}

		return branchesLoadedMsg{BranchesLoadedMsg{
			WorkspaceID:   workspaceID,
			Branches:      resp.Branches,
			CurrentBranch: resp.CurrentBranch,
			DefaultBranch: resp.DefaultBranch,
		}}
	}
}

func (c *client) fetchOriginBranches(workspaceID uint) tea.Cmd {
	return func() tea.Msg {
		fail := func(err error) tea.Msg {
			return branchesFetchedLoadedMsg{BranchesFetchedMsg{WorkspaceID: workspaceID, Err: err}}
		}
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/workspaces/%d/branches/fetch", c.baseURL, workspaceID), nil)
		if err != nil {
			return fail(err)
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] fetch origin branches: %v", err)
			return fail(err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return fail(parseAPIError(res, "fetch origin branches").Err)
		}

		var resp workspace.BranchRefListResponse
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			return fail(err)
		}

		return branchesFetchedLoadedMsg{BranchesFetchedMsg{WorkspaceID: workspaceID, Branches: resp.Branches, DefaultBranch: resp.DefaultBranch}}
	}
}

func (c *client) checkBranchExists(workspaceID uint, name string) tea.Cmd {
	return func() tea.Msg {
		fail := func(err error) tea.Msg {
			return branchExistsCheckedLoadedMsg{BranchExistsCheckedMsg{WorkspaceID: workspaceID, Name: name, Err: err}}
		}
		u := fmt.Sprintf("%s/workspaces/%d/branches/exists?name=%s", c.baseURL, workspaceID, url.QueryEscape(name))
		res, err := c.httpClient.Get(u)
		if err != nil {
			log.Printf("[ERROR] check branch exists %q: %v", name, err)
			return fail(err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return fail(parseAPIError(res, "check branch exists").Err)
		}

		var resp branchExistsResponse
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			return fail(err)
		}

		return branchExistsCheckedLoadedMsg{BranchExistsCheckedMsg{
			WorkspaceID:  workspaceID,
			Name:         name,
			ExistsLocal:  resp.ExistsLocal,
			ExistsRemote: resp.ExistsRemote,
		}}
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

		return SessionSwitchedMsg{}
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

func (c *client) fetchBundles() tea.Cmd {
	return func() tea.Msg {
		fail := func(err error) tea.Msg {
			log.Printf("[ERROR] fetch bundles: %v", err)
			return BundlesLoadedMsg{Err: err}
		}
		res, err := c.httpClient.Get(c.baseURL + "/bundles")
		if err != nil {
			return fail(err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return BundlesLoadedMsg{Err: parseAPIError(res, "fetch bundles").Err}
		}

		var resp bundles.ListResponse
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			return fail(err)
		}
		return BundlesLoadedMsg{Bundles: resp.Bundles}
	}
}

func (c *client) fetchDefaultBundles(ids []uint) tea.Cmd {
	return func() tea.Msg {
		fail := func(err error) tea.Msg {
			log.Printf("[ERROR] fetch default bundles %v: %v", ids, err)
			return DefaultBundlesLoadedMsg{WorkspaceIDs: ids, Err: err}
		}
		q := url.Values{}
		for _, id := range ids {
			q.Add("workspace_id", strconv.FormatUint(uint64(id), 10))
		}
		res, err := c.httpClient.Get(c.baseURL + "/sessions/default-bundles?" + q.Encode())
		if err != nil {
			return fail(err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return DefaultBundlesLoadedMsg{WorkspaceIDs: ids, Err: parseAPIError(res, "fetch default bundles").Err}
		}

		var resp session.DefaultBundlesResponse
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			return fail(err)
		}
		return DefaultBundlesLoadedMsg{WorkspaceIDs: ids, Bundles: resp.Bundles}
	}
}

func (c *client) setSessionBundles(id uint, names []string) tea.Cmd {
	return func() tea.Msg {
		if names == nil {
			names = []string{}
		}
		body, err := json.Marshal(map[string][]string{"bundles": names})
		if err != nil {
			return ErrMsg{err}
		}
		req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/sessions/%d/bundles", c.baseURL, id), bytes.NewReader(body))
		if err != nil {
			return ErrMsg{err}
		}
		req.Header.Set("Content-Type", "application/json")

		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] set session bundles %d: %v", id, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "set session bundles")
		}

		return SessionBundlesSetMsg{SessionID: id}
	}
}

func (c *client) fetchCoderStatus() tea.Cmd {
	return func() tea.Msg {
		res, err := c.httpClient.Get(c.baseURL + "/coder/status")
		if err != nil {
			log.Printf("[ERROR] fetch coder status: %v", err)
			return CoderStatusLoadedMsg{Err: err}
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return CoderStatusLoadedMsg{}
		}
		var resp struct {
			Configured bool `json:"configured"`
		}
		if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
			return CoderStatusLoadedMsg{Err: err}
		}
		return CoderStatusLoadedMsg{Configured: resp.Configured}
	}
}

func (c *client) createSession(name string, specs []SessionWorkspaceSpec, selected []string, backend string) tea.Cmd {
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
		if selected != nil {
			body["bundles"] = selected
		}
		if backend != "" {
			body["backend"] = backend
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

func (c *client) addBlocker(sessionID uint, reqBody session.AddBlockerRequest) tea.Cmd {
	return func() tea.Msg {
		body, err := json.Marshal(reqBody)
		if err != nil {
			return ErrMsg{err}
		}
		res, err := c.httpClient.Post(fmt.Sprintf("%s/sessions/%d/blockers", c.baseURL, sessionID), "application/json", bytes.NewReader(body))
		if err != nil {
			log.Printf("[ERROR] add blocker %d: %v", sessionID, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusCreated {
			return parseAPIError(res, "add blocker")
		}
		return blockerAddedMsg{sessionID: sessionID}
	}
}

func (c *client) resolveBlocker(sessionID, blockerID uint) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/sessions/%d/blockers/%d/resolve", c.baseURL, sessionID, blockerID), nil)
		if err != nil {
			return ErrMsg{err}
		}
		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] resolve blocker %d: %v", blockerID, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "resolve blocker")
		}
		return blockerChangedMsg{}
	}
}

func (c *client) deleteBlocker(sessionID, blockerID uint) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/sessions/%d/blockers/%d", c.baseURL, sessionID, blockerID), nil)
		if err != nil {
			return ErrMsg{err}
		}
		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] delete blocker %d: %v", blockerID, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusNoContent {
			return parseAPIError(res, "delete blocker")
		}
		return blockerChangedMsg{}
	}
}

func (c *client) fetchBrowserState() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/browser/sessions", nil)
		if err != nil {
			return nil
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil
		}
		defer res.Body.Close()

		empty := browser.SessionsView{Sessions: map[uint]browser.SessionBrowser{}}
		switch res.StatusCode {
		case http.StatusOK:
		case http.StatusNotFound:
			return browserStateLoadedMsg{state: empty}
		default:
			return nil
		}
		var view browser.SessionsView
		if err := json.NewDecoder(res.Body).Decode(&view); err != nil {
			return nil
		}
		if view.Sessions == nil {
			view.Sessions = empty.Sessions
		}
		return browserStateLoadedMsg{state: view}
	}
}

func (c *client) focusTab(sessionID uint, windowID, tabID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		out, err := browser.RequestFocus(ctx, http.DefaultClient, c.baseURL+"/browser", browser.FocusRequest{SessionID: sessionID, WindowID: windowID, TabID: tabID})
		if err != nil {
			log.Printf("[ERROR] focus tab %s/%s: %v", windowID, tabID, err)
			return browserFocusFailedMsg{err}
		}
		if out.Activated {
			return SessionSwitchedMsg{}
		}
		return browserFocusedMsg{}
	}
}

func (c *client) openURL(sessionID uint, link string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		req := browser.OpenRequest{SessionID: sessionID, URL: link, Activate: true}
		res, err := browser.RequestOpen(ctx, http.DefaultClient, c.baseURL+"/browser", req)
		if err != nil {
			log.Printf("[WARN] browser open via daemon failed, falling back: %v", err)
			return ui.OpenURL(link)()
		}
		if res.Activated {
			return SessionSwitchedMsg{}
		}
		return nil
	}
}
