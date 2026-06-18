package sessionprogress

import (
	"strings"
	"testing"

	"github.com/eleonorayaya/utena/internal/session"
)

func creatingModelWithSteps(steps []session.SessionSetupStep) Model {
	m := New()
	m.session = &session.Session{
		Name:       "demo",
		Status:     session.StatusCreating,
		SetupSteps: steps,
	}
	return m
}

func TestRenderSteps_AllStatuses(t *testing.T) {
	m := creatingModelWithSteps([]session.SessionSetupStep{
		{Label: "Create session directory", Status: session.SetupStepDone},
		{Label: "Setup workspace: api", Status: session.SetupStepRunning},
		{Label: "Install dependencies: web", Status: session.SetupStepPending},
		{Label: "Copy .env.example → .env", Status: session.SetupStepFailed, ErrorMsg: "source not found"},
	})

	out := m.renderSteps()

	for _, label := range []string{
		"Create session directory",
		"Setup workspace: api",
		"Install dependencies: web",
		"Copy .env.example → .env",
	} {
		if !strings.Contains(out, label) {
			t.Errorf("expected rendered steps to contain %q\n%s", label, out)
		}
	}

	if !strings.Contains(out, "✓") {
		t.Errorf("expected a done glyph (✓):\n%s", out)
	}
	if !strings.Contains(out, "✗") {
		t.Errorf("expected a failed glyph (✗):\n%s", out)
	}
	if !strings.Contains(out, "○") {
		t.Errorf("expected a pending glyph (○):\n%s", out)
	}
	if !strings.Contains(out, "source not found") {
		t.Errorf("expected failed step error message to render:\n%s", out)
	}
}

func TestView_StepsRenderedDuringCreating(t *testing.T) {
	m := creatingModelWithSteps([]session.SessionSetupStep{
		{Label: "Create session directory", Status: session.SetupStepDone},
	})

	out := m.View()
	if !strings.Contains(out, "Create session directory") {
		t.Errorf("expected View to render the step checklist:\n%s", out)
	}
	if strings.Contains(out, "Setting up...") {
		t.Errorf("expected View NOT to fall back to the spinner when steps exist:\n%s", out)
	}
}

func TestView_FallsBackToSpinnerWithoutSteps(t *testing.T) {
	m := creatingModelWithSteps(nil)

	out := m.View()
	if !strings.Contains(out, "Setting up...") {
		t.Errorf("expected spinner fallback for sessions without steps:\n%s", out)
	}
}
