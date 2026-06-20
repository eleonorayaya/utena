package session

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/eleonorayaya/utena/internal/workspace"
)

func assignedToStepWorkspaces(assigned []assignedSlot) []stepWorkspace {
	out := make([]stepWorkspace, len(assigned))
	for i, a := range assigned {
		out[i] = stepWorkspace{ID: a.ws.ID, Name: a.ws.Name, Path: a.ws.Path}
	}
	return out
}

func (s *SessionService) loadWorkspaceConfigs(wss []stepWorkspace) map[uint]*WorkspaceConfig {
	configs := make(map[uint]*WorkspaceConfig, len(wss))
	for _, ws := range wss {
		cfg, err := loadWorkspaceConfig(ws.Path)
		if err != nil {
			slog.Warn("failed to load workspace config", "ws", ws.Name, "path", ws.Path, "error", err)
		}
		configs[ws.ID] = cfg
	}
	return configs
}

func newSetupStep(sessionID uint, pos int, d stepDef) *SessionSetupStep {
	return &SessionSetupStep{
		SessionID:   sessionID,
		Position:    pos,
		Kind:        d.Kind,
		WorkspaceID: d.WorkspaceID,
		Label:       d.Label,
		Status:      SetupStepPending,
	}
}

func (s *SessionService) persistSetupSteps(sessionID uint, defs []stepDef) ([]*SessionSetupStep, error) {
	steps := make([]*SessionSetupStep, len(defs))
	for i, d := range defs {
		steps[i] = newSetupStep(sessionID, i, d)
	}
	if err := s.setupStepStore.Create(steps); err != nil {
		return nil, err
	}
	for i, d := range defs {
		if d.DependsOnPos == nil {
			continue
		}
		depID := steps[*d.DependsOnPos].ID
		steps[i].DependsOnID = &depID
		if err := s.setupStepStore.Update(steps[i]); err != nil {
			return nil, err
		}
	}
	return steps, nil
}

func stepsMatchDefs(steps []*SessionSetupStep, defs []stepDef) bool {
	if len(steps) != len(defs) {
		return false
	}
	for i := range defs {
		if steps[i].Kind != defs[i].Kind {
			return false
		}
	}
	return true
}

func (s *SessionService) ensureSetupSteps(sessionID uint, defs []stepDef) []*SessionSetupStep {
	existing, err := s.setupStepStore.ListBySessionID(sessionID)
	if err == nil && stepsMatchDefs(existing, defs) {
		if resetErr := s.setupStepStore.ResetAllForSession(sessionID); resetErr != nil {
			slog.Warn("failed to reset setup steps", "session", sessionID, "error", resetErr)
		}
		for _, st := range existing {
			st.Status = SetupStepPending
			st.ErrorMsg = ""
		}
		return existing
	}

	if err == nil && len(existing) > 0 {
		if delErr := s.setupStepStore.DeleteBySessionID(sessionID); delErr != nil {
			slog.Warn("failed to delete stale setup steps", "session", sessionID, "error", delErr)
		}
	}

	steps, createErr := s.persistSetupSteps(sessionID, defs)
	if createErr != nil {
		slog.Warn("failed to persist setup steps; progress tracking disabled for session", "session", sessionID, "error", createErr)
		steps = make([]*SessionSetupStep, len(defs))
		for i, d := range defs {
			steps[i] = newSetupStep(sessionID, i, d)
		}
	}
	return steps
}

type stepUpdater struct {
	store *SessionSetupStepStore
	mu    sync.Mutex
}

func (u *stepUpdater) set(step *SessionSetupStep, status SetupStepStatus, errMsg string) {
	step.Status = status
	step.ErrorMsg = errMsg
	if step.ID == 0 {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if err := u.store.Update(step); err != nil {
		slog.Warn("failed to update setup step status", "step", step.ID, "status", status, "error", err)
	}
}

type execStep struct {
	def stepDef
	db  *SessionSetupStep
}

type setupExecution struct {
	svc       *SessionService
	sess      *Session
	tmuxName  string
	execSteps []execStep
	orderedWS []stepWorkspace
	specByWS  map[uint]WorkspaceBranchSpec
	wsByID    map[uint]*workspace.Workspace
	swtByID   map[uint]*SessionWorktree
	updater   *stepUpdater

	mu          sync.Mutex
	results     map[uint]*worktreeSetupResult
	warnings    []string
	broken      bool
	brokenStage string
	brokenErr   error
}

func isTerminalStatus(status SetupStepStatus) bool {
	return status == SetupStepDone || status == SetupStepFailed
}

func (e *setupExecution) addWarning(msg string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.warnings = append(e.warnings, msg)
}

func (e *setupExecution) setResult(wsID uint, r *worktreeSetupResult) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.results[wsID] = r
}

func (e *setupExecution) anyWorktreeCreated() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range e.results {
		if r != nil && r.created {
			return true
		}
	}
	return false
}

func (e *setupExecution) getResult(wsID uint) *worktreeSetupResult {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.results[wsID]
}

func (e *setupExecution) setBroken(stage string, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.broken {
		return
	}
	e.broken = true
	e.brokenStage = stage
	e.brokenErr = err
}

func (e *setupExecution) isBroken() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.broken
}

func (e *setupExecution) ready(idx int) bool {
	es := e.execSteps[idx]
	if es.db.Status != SetupStepPending {
		return false
	}
	switch {
	case es.def.Kind == StepKindCreateSessionDir:
		return true
	case es.def.Kind == StepKindApplyClaude && es.def.DependsOnPos == nil:
		for i := range e.execSteps {
			o := e.execSteps[i]
			if o.def.WorkspaceID != nil && !isTerminalStatus(o.db.Status) {
				return false
			}
		}
		return true
	case es.def.DependsOnPos != nil:
		dep := e.execSteps[*es.def.DependsOnPos]
		return isTerminalStatus(dep.db.Status)
	default:
		return false
	}
}

func (e *setupExecution) run(ctx context.Context) {
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	for {
		if execCtx.Err() != nil {
			return
		}

		var ready []int
		for i := range e.execSteps {
			if e.ready(i) {
				ready = append(ready, i)
			}
		}
		if len(ready) == 0 {
			return
		}

		var wg sync.WaitGroup
		for _, idx := range ready {
			es := &e.execSteps[idx]
			e.updater.set(es.db, SetupStepRunning, "")
			wg.Add(1)
			go func() {
				defer wg.Done()
				critical, err := e.executeStep(execCtx, es.def)
				if err != nil {
					e.updater.set(es.db, SetupStepFailed, err.Error())
					if critical {
						e.setBroken(string(es.def.Kind), err)
						cancel()
					} else {
						e.addWarning(err.Error())
					}
					return
				}
				e.updater.set(es.db, SetupStepDone, "")
			}()
		}
		wg.Wait()

		if e.isBroken() {
			return
		}
	}
}

func (e *setupExecution) executeStep(ctx context.Context, def stepDef) (bool, error) {
	switch def.Kind {
	case StepKindCreateSessionDir:
		if e.sess.SessionRoot != "" {
			if err := os.MkdirAll(e.sess.SessionRoot, 0o755); err != nil {
				return true, fmt.Errorf("ensure session root: %w", err)
			}
		}
		return false, nil
	case StepKindSetupWorktree:
		return e.execSetupWorktree(ctx, def)
	case StepKindCopyFile:
		return false, e.execCopyFile(def)
	case StepKindInstallDeps:
		return false, e.execInstallDeps(ctx, def)
	case StepKindApplyClaude:
		return false, e.execApplyClaude(ctx)
	case StepKindSetupTmux:
		if err := e.svc.setupTmux(ctx, e.sess, e.tmuxName, e.sess.SessionRoot); err != nil {
			return true, fmt.Errorf("tmux setup: %w", err)
		}
		return false, nil
	}
	return false, nil
}

func (e *setupExecution) execSetupWorktree(ctx context.Context, def stepDef) (bool, error) {
	wsID := *def.WorkspaceID
	ws := e.wsByID[wsID]
	swt := e.swtByID[wsID]

	result, err := e.svc.setupSingleWorktree(ctx, e.sess, swt, ws, e.specByWS[wsID])
	if err != nil {
		return true, fmt.Errorf("worktree setup: %w", err)
	}
	if result.warning != "" {
		e.addWarning(fmt.Sprintf("%s: %s", ws.Name, result.warning))
	}
	e.setResult(wsID, &result)

	if result.created {
		branchName := ""
		if result.branch != nil {
			branchName = result.branch.Name
		}
		env := []string{
			envSessionRoot + "=" + e.sess.SessionRoot,
			envSessionName + "=" + e.sess.Name,
			envBranch + "=" + branchName,
			envWorktreePath + "=" + result.worktreePath,
			envWorkspaceName + "=" + ws.Name,
			envWorkspacePath + "=" + ws.Path,
		}
		wsScript := filepath.Join(ws.Path, ".utena", worktreeSetupName)
		if scriptErr := traceOp(ctx, "worktree-setup script", func() error {
			return e.svc.runScript(ctx, wsScript, result.worktreePath, env)
		}, "script", wsScript, "ws", ws.Name); scriptErr != nil {
			slog.WarnContext(ctx, "workspace worktree-setup script failed", "ws", ws.Name, "error", scriptErr)
			e.addWarning(fmt.Sprintf("%s: %s", ws.Name, scriptErr.Error()))
		}
	}
	return false, nil
}

func (e *setupExecution) execCopyFile(def stepDef) error {
	wsID := *def.WorkspaceID
	ws := e.wsByID[wsID]
	result := e.getResult(wsID)
	worktreePath := ""
	if result != nil {
		worktreePath = result.worktreePath
	}

	if err := validateRelPath(def.CopySrc); err != nil {
		return fmt.Errorf("copy_file src %q: %w", def.CopySrc, err)
	}
	if err := validateRelPath(def.CopyDest); err != nil {
		return fmt.Errorf("copy_file dest %q: %w", def.CopyDest, err)
	}

	base := worktreePath
	if def.CopyDestRelTo == DestRelativeToSessionRoot {
		base = e.sess.SessionRoot
	}
	if base == "" {
		return fmt.Errorf("copy_file %q: destination base path unavailable", def.CopyDest)
	}

	srcPath := filepath.Join(ws.Path, def.CopySrc)
	data, err := os.ReadFile(srcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("copy_file source not found: %s", srcPath)
		}
		return fmt.Errorf("copy_file read %s: %w", srcPath, err)
	}

	destPath := filepath.Join(base, def.CopyDest)
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("copy_file mkdir %s: %w", filepath.Dir(destPath), err)
	}
	if err := os.WriteFile(destPath, data, 0o644); err != nil {
		return fmt.Errorf("copy_file write %s: %w", destPath, err)
	}
	return nil
}

func (e *setupExecution) execInstallDeps(ctx context.Context, def stepDef) error {
	wsID := *def.WorkspaceID
	result := e.getResult(wsID)
	if result == nil || result.worktreePath == "" {
		return fmt.Errorf("install_deps: worktree path unavailable")
	}
	worktreePath := result.worktreePath

	pm := def.PackageManager
	if pm == "" || pm == packageManagerAuto {
		pm = detectPackageManager(worktreePath)
	}

	return traceOp(ctx, "install_deps", func() error {
		cmd := exec.CommandContext(ctx, pm, "install")
		cmd.Dir = worktreePath
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s install failed: %s: %w", pm, strings.TrimSpace(string(output)), err)
		}
		return nil
	}, "pm", pm, "dir", worktreePath)
}

func (e *setupExecution) execApplyClaude(ctx context.Context) error {
	if e.anyWorktreeCreated() {
		globalBranch := ""
		if len(e.orderedWS) > 0 {
			if r := e.getResult(e.orderedWS[0].ID); r != nil && r.branch != nil {
				globalBranch = r.branch.Name
			}
		}
		globalEnv := []string{
			envSessionRoot + "=" + e.sess.SessionRoot,
			envSessionName + "=" + e.sess.Name,
			envBranch + "=" + globalBranch,
		}
		globalScript := filepath.Join(e.svc.configDir, worktreeSetupName)
		if err := traceOp(ctx, "global worktree-setup script", func() error {
			return e.svc.runScript(ctx, globalScript, e.sess.SessionRoot, globalEnv)
		}, "script", globalScript); err != nil {
			slog.WarnContext(ctx, "global worktree-setup script failed", "error", err)
			e.addWarning(fmt.Sprintf("global: %s", err.Error()))
		}
	}

	for _, w := range e.svc.applyClaudeSettings(ctx, e.sess) {
		e.addWarning(w)
	}
	return nil
}

func validateRelPath(p string) error {
	if p == "" {
		return fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("path must be relative")
	}
	if strings.Contains(p, "..") {
		return fmt.Errorf("path must not contain '..'")
	}
	return nil
}
