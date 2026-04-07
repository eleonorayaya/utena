package workspace

import (
	"fmt"
	"net/http"

	"github.com/eleonorayaya/utena/internal/git"
	"github.com/go-chi/render"
)

type WorkspaceResponse struct {
	*Workspace
}

func NewWorkspaceResponse(workspace *Workspace) *WorkspaceResponse {
	return &WorkspaceResponse{Workspace: workspace}
}

func (wr *WorkspaceResponse) Render(w http.ResponseWriter, r *http.Request) error {

	return nil
}

type WorkspaceListResponse struct {
	Workspaces []Workspace `json:"workspaces"`
}

func NewWorkspaceListResponse(workspaces []Workspace) *WorkspaceListResponse {
	return &WorkspaceListResponse{Workspaces: workspaces}
}

func (wlr *WorkspaceListResponse) Render(w http.ResponseWriter, r *http.Request) error {

	return nil
}

func RenderWorkspaceList(workspaces []Workspace) []render.Renderer {
	list := make([]render.Renderer, len(workspaces))
	for i, workspace := range workspaces {
		ws := workspace
		list[i] = NewWorkspaceResponse(&ws)
	}
	return list
}

type AddWorkspaceRequest struct {
	Path   string `json:"path"`
	AsRoot bool   `json:"as_root"`
}

func (a *AddWorkspaceRequest) Bind(r *http.Request) error {
	if a.Path == "" {
		return fmt.Errorf("path is required")
	}
	return nil
}

type BranchListResponse struct {
	Branches      []string `json:"branches"`
	CurrentBranch string   `json:"current_branch"`
}

type PRListResponse struct {
	PullRequests []git.PullRequest `json:"pull_requests"`
}
