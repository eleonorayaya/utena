package session

import (
	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

type SetupStepStatus string

const (
	SetupStepPending SetupStepStatus = "pending"
	SetupStepRunning SetupStepStatus = "running"
	SetupStepDone    SetupStepStatus = "done"
	SetupStepFailed  SetupStepStatus = "failed"
)

type SetupStepKind string

const (
	StepKindCreateSessionDir SetupStepKind = "create_session_dir"
	StepKindSetupWorktree    SetupStepKind = "setup_worktree"
	StepKindCopyFile         SetupStepKind = "copy_file"
	StepKindInstallDeps      SetupStepKind = "install_deps"
	StepKindApplyClaude      SetupStepKind = "apply_claude"
	StepKindSetupTmux        SetupStepKind = "setup_tmux"
)

type SessionSetupStep struct {
	gorm.Model
	SessionID   uint            `json:"session_id" gorm:"index;not null"`
	Position    int             `json:"position" gorm:"not null"`
	Kind        SetupStepKind   `json:"kind" gorm:"not null"`
	WorkspaceID *uint           `json:"workspace_id,omitempty" gorm:"index"`
	DependsOnID *uint           `json:"depends_on_id,omitempty" gorm:"index"`
	Label       string          `json:"label" gorm:"not null"`
	Status      SetupStepStatus `json:"status" gorm:"not null;default:'pending'"`
	ErrorMsg    string          `json:"error_msg,omitempty" gorm:"type:text"`
}

type SessionSetupStepStore struct {
	db db.Database
}

func NewSessionSetupStepStore(database db.Database) *SessionSetupStepStore {
	return &SessionSetupStepStore{db: database}
}

func (s *SessionSetupStepStore) Create(steps []*SessionSetupStep) error {
	if len(steps) == 0 {
		return nil
	}
	return s.db.Create(steps).Error
}

func (s *SessionSetupStepStore) Update(step *SessionSetupStep) error {
	return s.db.Save(step).Error
}

func (s *SessionSetupStepStore) ListBySessionID(sessionID uint) ([]*SessionSetupStep, error) {
	var steps []*SessionSetupStep
	if err := s.db.Where("session_id = ?", sessionID).Order("position ASC").Find(&steps).Error; err != nil {
		return nil, err
	}
	return steps, nil
}

func (s *SessionSetupStepStore) DeleteBySessionID(sessionID uint) error {
	return s.db.Where("session_id = ?", sessionID).Delete(&SessionSetupStep{}).Error
}

func (s *SessionSetupStepStore) ResetAllForSession(sessionID uint) error {
	return s.db.Model(&SessionSetupStep{}).
		Where("session_id = ?", sessionID).
		Updates(map[string]any{"status": SetupStepPending, "error_msg": ""}).Error
}
