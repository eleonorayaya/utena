package session

import (
	"fmt"

	"github.com/eleonorayaya/utena/internal/workspace"
)

type stepWorkspace struct {
	ID                uint
	Name              string
	Path              string
	LeaderWorkspaceID *uint
}

func newStepWorkspace(ws *workspace.Workspace) stepWorkspace {
	return stepWorkspace{ID: ws.ID, Name: ws.Name, Path: ws.Path, LeaderWorkspaceID: ws.LeaderWorkspaceID}
}

func (w stepWorkspace) workspace() workspace.Workspace {
	ws := workspace.Workspace{Name: w.Name, Path: w.Path, LeaderWorkspaceID: w.LeaderWorkspaceID}
	ws.ID = w.ID
	return ws
}

type stepDef struct {
	Kind         SetupStepKind
	Label        string
	WorkspaceID  *uint
	DependsOnPos *int

	CopySrc       string
	CopyDest      string
	CopyDestRelTo DestRelativeTo

	PackageManager string
	RunWith        []string
}

func intPtr(i int) *int    { return &i }
func uintPtr(u uint) *uint { return &u }

func buildSetupSteps(workspaces []stepWorkspace, configs map[uint]*WorkspaceConfig) []stepDef {
	defs := buildWorkspaceSetupSteps(workspaces, configs)
	return append(defs, stepDef{
		Kind:         StepKindSetupMux,
		Label:        setupMuxLabel,
		DependsOnPos: intPtr(len(defs) - 1),
	})
}

func buildWorkspaceSetupSteps(workspaces []stepWorkspace, configs map[uint]*WorkspaceConfig) []stepDef {
	defs := make([]stepDef, 0, len(workspaces)*2+3)

	defs = append(defs, stepDef{Kind: StepKindCreateSessionDir, Label: "Create session directory"})
	sessionDirPos := 0

	for _, ws := range workspaces {
		defs = append(defs, stepDef{
			Kind:         StepKindSetupWorktree,
			Label:        fmt.Sprintf("Setup workspace: %s", ws.Name),
			WorkspaceID:  uintPtr(ws.ID),
			DependsOnPos: intPtr(sessionDirPos),
		})
		lastPos := len(defs) - 1

		defs = appendActionSteps(defs, ws, configs[ws.ID], lastPos)
	}

	return append(defs, stepDef{Kind: StepKindApplyClaude, Label: "Apply Claude settings"})
}

func appendActionSteps(defs []stepDef, ws stepWorkspace, cfg *WorkspaceConfig, lastPos int) []stepDef {
	if cfg == nil {
		return defs
	}

	for _, action := range cfg.SetupActions {
		if action.Type != WorkspaceActionCopyFile {
			continue
		}
		defs = append(defs, stepDef{
			Kind:          StepKindCopyFile,
			Label:         fmt.Sprintf("Copy %s → %s", action.Src, action.Dest),
			WorkspaceID:   uintPtr(ws.ID),
			DependsOnPos:  intPtr(lastPos),
			CopySrc:       action.Src,
			CopyDest:      action.Dest,
			CopyDestRelTo: action.DestRelativeTo,
		})
		lastPos = len(defs) - 1
	}

	for _, action := range cfg.SetupActions {
		if action.Type != WorkspaceActionInstallDeps {
			continue
		}
		label := fmt.Sprintf("Install dependencies: %s", ws.Name)
		if action.PackageManager != "" && action.PackageManager != packageManagerAuto {
			label = fmt.Sprintf("Install dependencies: %s (%s)", ws.Name, action.PackageManager)
		}
		defs = append(defs, stepDef{
			Kind:           StepKindInstallDeps,
			Label:          label,
			WorkspaceID:    uintPtr(ws.ID),
			DependsOnPos:   intPtr(lastPos),
			PackageManager: action.PackageManager,
			RunWith:        action.RunWith,
		})
		lastPos = len(defs) - 1
	}
	return defs
}
