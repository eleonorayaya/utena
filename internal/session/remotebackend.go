package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/eleonorayaya/utena/internal/bundles"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/eventbus"
)

var ErrCoderNotConfigured = common.NewInvalidRequest("coder backend is not configured: set coder.template in daemon.json")
var ErrCoderUnsupported = common.NewInvalidRequest("not supported for coder sessions")

const remoteOpTimeout = 20 * time.Minute

var MirroredStatuses = []SessionStatus{StatusActive, StatusInactive, StatusBroken, StatusCompleted}

type RemoteCreate struct {
	SessionID         uint
	Workspace         string
	Repo              string
	LeaderWorkspaceID uint
	Spec              WorkspaceBranchSpec
	Bundles           []string
}

type RemoteState struct {
	Status      SessionStatus
	StatusError string
	Attention   claude.ClaudeSessionStatus
	Branches    []string
	SyncedAt    time.Time
}

type RemoteBackend interface {
	Enabled() bool
	WorkspaceName(name string, taken []string) (string, error)
	Create(ctx context.Context, in RemoteCreate) error
	Activate(ctx context.Context, sess *Session, resume bool) (string, error)
	Archive(ctx context.Context, sess *Session) error
	Delete(ctx context.Context, sess *Session) error
}

func (s *SessionService) SetRemoteBackend(backend RemoteBackend) {
	s.remote = backend
}

func (s *SessionService) CoderEnabled() bool {
	return s.remote != nil && s.remote.Enabled()
}

func (s *SessionService) CoderSessions() ([]Session, error) {
	return s.store.ListCoder()
}

func (s *SessionService) SetRemoteSessionID(id, remoteID uint) error {
	return s.store.SetRemoteSessionID(id, remoteID)
}

func (s *SessionService) ApplyRemoteState(id uint, st RemoteState) error {
	if err := s.store.ApplyRemoteState(id, st, MirroredStatuses); err != nil {
		return err
	}
	return s.recordRemoteBranches(context.Background(), id, st.Branches)
}

func (s *SessionService) recordRemoteBranches(ctx context.Context, id uint, names []string) error {
	sess, err := s.store.GetByID(id)
	if err != nil {
		return err
	}
	if sess.RemoteRepoID == nil {
		return nil
	}
	recorded := make(map[string]bool, len(sess.Branches))
	for _, sb := range sess.Branches {
		if sb.Branch != nil {
			recorded[sb.Branch.Name] = true
		}
	}
	var fresh []string
	for _, name := range names {
		if name != "" && !recorded[name] && !slices.Contains(fresh, name) {
			fresh = append(fresh, name)
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	repo, err := s.gitService.GetRepo(*sess.RemoteRepoID)
	if err != nil {
		return fmt.Errorf("load repo for coder session %d: %w", id, err)
	}
	defaultBranch := s.gitService.DefaultBranch(ctx, repo.Path)
	now := time.Now()
	for _, name := range fresh {
		if name == defaultBranch {
			continue
		}
		branch, err := s.gitService.FindOrCreateBranch(ctx, name, repo.ID)
		if err != nil {
			return fmt.Errorf("find-or-create branch %q: %w", name, err)
		}
		if err := s.sessionBranchStore.Add(&SessionBranch{SessionID: id, BranchID: branch.ID, FirstSeenAt: now}); err != nil {
			return fmt.Errorf("record session branch: %w", err)
		}
		for _, event := range openPRNotifications(id, s.gitService.GetPRsForBranch(branch.ID), func(uint) string { return branch.Name }) {
			s.publishNotification(ctx, event.SessionID, event.Type, event.Data)
		}
	}
	return nil
}

func (s *SessionService) createCoderSession(ctx context.Context, name string, slot assignedSlot, input CreateSessionInput) (*Session, error) {
	if !s.CoderEnabled() {
		return nil, ErrCoderNotConfigured
	}
	repoID, err := s.resolveRepoID(ctx, slot.ws)
	if err != nil {
		return nil, common.NewInvalidRequest(err.Error())
	}
	repo, err := s.gitService.GetRepo(repoID)
	if err != nil {
		return nil, fmt.Errorf("load repo for workspace %q: %w", slot.ws.Name, err)
	}
	if _, _, err := repo.OwnerAndName(); err != nil {
		return nil, common.NewInvalidRequest(fmt.Sprintf("workspace %q has no GitHub origin: %v", slot.ws.Name, err))
	}
	taken, err := s.store.CoderWorkspaceNames()
	if err != nil {
		return nil, fmt.Errorf("list coder workspaces: %w", err)
	}
	wsName, err := s.remote.WorkspaceName(name, taken)
	if err != nil {
		return nil, common.NewInvalidRequest(err.Error())
	}

	sess := &Session{
		Name:           name,
		Status:         StatusCreating,
		LastUsedAt:     time.Now(),
		Bundles:        bundles.Union(input.Bundles),
		Backend:        BackendCoder,
		CoderWorkspace: wsName,
		RemoteRepoID:   &repoID,
	}
	if err := s.store.Add(sess); err != nil {
		return nil, err
	}
	if err := s.workspaceService.Touch(ctx, slot.ws.ID); err != nil {
		slog.WarnContext(ctx, "failed to touch workspace last-used timestamp", "workspace", slot.ws.ID, "error", err)
	}

	in := RemoteCreate{
		SessionID:         sess.ID,
		Workspace:         wsName,
		Repo:              repo.FullName,
		LeaderWorkspaceID: slot.ws.ID,
		Spec:              slot.spec,
		Bundles:           input.Bundles,
	}
	go s.runCoderCreate(in)
	return sess, nil
}

func (s *SessionService) runCoderCreate(in RemoteCreate) {
	ctx, cancel := context.WithTimeout(context.Background(), remoteOpTimeout)
	defer cancel()
	err := s.remote.Create(ctx, in)
	if err != nil {
		slog.Warn("coder session create failed", "session", in.SessionID, "workspace", in.Workspace, "error", err)
		if _, updErr := s.store.UpdateStatusWhere(in.SessionID, StatusBroken, err.Error(), StatusCreating); updErr != nil {
			slog.Error("failed to persist broken coder session", "session", in.SessionID, "error", updErr)
		}
		return
	}
	if _, err := s.store.UpdateStatusWhere(in.SessionID, StatusActive, "", StatusCreating); err != nil {
		slog.Error("failed to persist active coder session", "session", in.SessionID, "error", err)
	}
}

func (s *SessionService) activateCoderSession(ctx context.Context, sess *Session) (*Session, error) {
	if !s.CoderEnabled() {
		return nil, ErrCoderNotConfigured
	}
	if sess.Status == StatusDeleted {
		return nil, ErrCannotActivate
	}
	resume := sess.Status == StatusArchived
	if sess.IsAttached && !resume {
		return sess, nil
	}
	if resume {
		ok, err := s.store.UpdateStatusWhere(sess.ID, StatusCreating, "", StatusArchived)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrCannotActivate
		}
		sess.Status = StatusCreating
		sess.StatusError = ""
	}
	snapshot := *sess
	go s.runCoderActivate(&snapshot, resume)
	return sess, nil
}

func (s *SessionService) runCoderActivate(sess *Session, resume bool) {
	ctx, cancel := context.WithTimeout(context.Background(), remoteOpTimeout)
	defer cancel()
	host, err := s.remote.Activate(ctx, sess, resume)
	if err != nil {
		slog.Warn("coder session activate failed", "session", sess.ID, "workspace", sess.CoderWorkspace, "error", err)
		msg := "activate failed: " + err.Error()
		if resume {
			if _, updErr := s.store.UpdateStatusWhere(sess.ID, StatusArchived, msg, StatusCreating); updErr != nil {
				slog.Error("failed to restore archived coder session", "session", sess.ID, "error", updErr)
			}
			return
		}
		if updErr := s.store.SetStatusError(sess.ID, msg, MirroredStatuses...); updErr != nil {
			slog.Error("failed to persist coder activate error", "session", sess.ID, "error", updErr)
		}
		return
	}
	if resume {
		if _, err := s.store.UpdateStatusWhere(sess.ID, StatusActive, "", StatusCreating); err != nil {
			slog.Error("failed to persist reactivated coder session", "session", sess.ID, "error", err)
		}
	} else if err := s.store.SetStatusError(sess.ID, "", MirroredStatuses...); err != nil {
		slog.Warn("failed to clear coder session error", "session", sess.ID, "error", err)
	}
	if err := s.store.ClearAttachedExcept(sess.ID); err != nil {
		slog.Warn("failed to clear stale attached flags", "error", err)
	}
	if err := s.store.MarkAttached(sess.ID, time.Now()); err != nil {
		slog.Error("failed to mark coder session attached", "session", sess.ID, "error", err)
		return
	}
	bg := context.Background()
	s.publishSessionEvent(bg, eventbus.SessionAttached, sess.ID)
	if err := s.eventBus.Publish(bg, eventbus.Event{
		Type: eventbus.SessionActivated,
		Data: eventbus.SessionActivatedEvent{SessionName: sess.CoderWorkspace, Host: host},
	}); err != nil {
		slog.Warn("failed to publish session activated event", "session", sess.ID, "error", err)
	}
}

func (s *SessionService) archiveCoderSession(ctx context.Context, sess *Session) (*Session, error) {
	sess.Status = StatusArchived
	sess.IsAttached = false
	sess.StatusError = ""
	if err := s.store.Update(sess); err != nil {
		return nil, err
	}
	s.publishSessionEvent(ctx, eventbus.SessionArchived, sess.ID)
	if s.remote == nil {
		return sess, nil
	}
	snapshot := *sess
	go func() {
		opCtx, cancel := context.WithTimeout(context.Background(), remoteOpTimeout)
		defer cancel()
		if err := s.remote.Archive(opCtx, &snapshot); err != nil {
			slog.Warn("coder session archive failed", "session", snapshot.ID, "workspace", snapshot.CoderWorkspace, "error", err)
			if updErr := s.store.SetStatusError(snapshot.ID, "archive failed: "+err.Error(), StatusArchived); updErr != nil {
				slog.Error("failed to persist coder archive error", "session", snapshot.ID, "error", updErr)
			}
		}
	}()
	return sess, nil
}

func (s *SessionService) deleteCoderSession(ctx context.Context, sess *Session) error {
	if s.remote == nil {
		return ErrCoderNotConfigured
	}
	sess.Status = StatusDeleted
	sess.IsAttached = false
	if err := s.store.Update(sess); err != nil {
		return err
	}
	snapshot := *sess
	go s.runCoderDelete(&snapshot)
	return nil
}

func (s *SessionService) runCoderDelete(sess *Session) {
	ctx, cancel := context.WithTimeout(context.Background(), remoteOpTimeout)
	defer cancel()
	if err := s.remote.Delete(ctx, sess); err != nil {
		slog.Warn("coder session delete failed", "session", sess.ID, "workspace", sess.CoderWorkspace, "error", err)
		if _, updErr := s.store.UpdateStatusWhere(sess.ID, StatusArchived, "delete failed: "+err.Error(), StatusDeleted); updErr != nil {
			slog.Error("failed to restore coder session after delete failure", "session", sess.ID, "error", updErr)
		}
		return
	}
	if err := s.store.HardDelete(sess.ID); err != nil {
		slog.Error("failed to delete coder session row", "session", sess.ID, "error", err)
		return
	}
	s.publishSessionEvent(ctx, eventbus.SessionDeleted, sess.ID)
}

func (s *SessionService) resumeCoderDeletes(sessions []Session) {
	if s.remote == nil {
		return
	}
	for i := range sessions {
		sess := sessions[i]
		if sess.IsCoder() && sess.Status == StatusDeleted {
			go s.runCoderDelete(&sess)
		}
	}
}

func (s *SessionService) findSessionForHook(data eventbus.MuxHookEvent) (*Session, error) {
	if data.Host == "" {
		return s.findSessionByMuxName(data.SessionName)
	}
	sess, err := s.store.GetByCoderWorkspace(data.Host)
	if err != nil {
		return nil, err
	}
	if sess.CoderWorkspace != data.SessionName {
		return nil, errors.New("hook session name does not match the coder session")
	}
	return sess, nil
}
