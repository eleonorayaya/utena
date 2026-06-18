package session

import "fmt"

type stepWorkspace struct {
	ID   uint
	Name string
	Path string
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
}

func intPtr(i int) *int    { return &i }
func uintPtr(u uint) *uint { return &u }

func buildSetupSteps(workspaces []stepWorkspace, configs map[uint]*WorkspaceConfig) []stepDef {
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

		cfg := configs[ws.ID]
		if cfg == nil {
			continue
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
			})
			lastPos = len(defs) - 1
		}
	}

	defs = append(defs, stepDef{Kind: StepKindApplyClaude, Label: "Apply Claude settings"})
	applyClaudePos := len(defs) - 1

	defs = append(defs, stepDef{
		Kind:         StepKindSetupTmux,
		Label:        "Setup tmux",
		DependsOnPos: intPtr(applyClaudePos),
	})

	return defs
}
