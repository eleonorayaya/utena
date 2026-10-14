package sessionprogress

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/ui"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func polled(t *testing.T, start tea.Msg, s session.Session) Model {
	t.Helper()
	m, _ := New().Init()
	m.SetSize(60, 20)
	m, _ = m.Update(start)
	m, _ = m.Update(provider.SessionPolledMsg{Session: s})
	return m
}

func demo(status session.SessionStatus, steps ...session.SessionSetupStep) session.Session {
	return session.Session{Model: gorm.Model{ID: 7}, Name: "demo", Status: status, SetupSteps: steps}
}

func TestView_TitleCreatingVsRepairing(t *testing.T) {
	m := polled(t, StartMsg{SessionID: 7}, demo(session.StatusCreating))
	assert.True(t, strings.HasPrefix(m.View(), "Creating demo") || strings.Contains(m.View(), "Creating demo"))
	m = polled(t, StartMsg{SessionID: 7, Repairing: true}, demo(session.StatusCreating))
	assert.Contains(t, m.View(), "Repairing demo")
}

func TestView_StepsInPositionOrderWithGlyphs(t *testing.T) {
	m := polled(t, StartMsg{SessionID: 7}, demo(session.StatusCreating,
		session.SessionSetupStep{Position: 2, Label: "Start tuios session", Status: session.SetupStepPending},
		session.SessionSetupStep{Position: 0, Label: "Fetch api", Status: session.SetupStepDone},
		session.SessionSetupStep{Position: 1, Label: "Create worktree", Status: session.SetupStepFailed, ErrorMsg: "dirty tree"},
	))
	lines := strings.Split(m.View(), "\n")
	assert.Equal(t, "  ✓ Fetch api", lines[2])
	assert.Equal(t, "  ✕ Create worktree", lines[3])
	assert.Equal(t, "    dirty tree", lines[4])
	assert.Equal(t, "  · Start tuios session", lines[5])
}

func TestView_SettingUpBeforeSteps(t *testing.T) {
	m := polled(t, StartMsg{SessionID: 7}, demo(session.StatusCreating))
	assert.Contains(t, m.View(), "Setting up")
}

func TestStatus_WarningAndErrorGoToFooter(t *testing.T) {
	s := demo(session.StatusActive)
	s.StatusError = "copy .env failed"
	m := polled(t, StartMsg{SessionID: 7}, s)
	assert.Equal(t, ui.Status{Text: "⚠ copy .env failed", Kind: ui.StatusWarning}, m.Status())
	assert.NotContains(t, m.View(), "copy .env failed")

	b := demo(session.StatusBroken)
	b.StatusError = "boom"
	m = polled(t, StartMsg{SessionID: 7}, b)
	assert.Equal(t, ui.Status{Text: "✕ boom", Kind: ui.StatusError}, m.Status())
}

func TestKeys_RetryOnlyWhenRelevant(t *testing.T) {
	m := polled(t, StartMsg{SessionID: 7}, demo(session.StatusCreating))
	assert.False(t, m.Keys().ShortHelp()[0].Enabled())
	b := demo(session.StatusBroken)
	m = polled(t, StartMsg{SessionID: 7}, b)
	assert.True(t, m.Keys().ShortHelp()[0].Enabled())
}
