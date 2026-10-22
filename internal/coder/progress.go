package coder

import (
	"log/slog"
	"slices"

	"github.com/eleonorayaya/utena/internal/session"
)

const (
	stepBuild   session.SetupStepKind = "coder_build"
	stepAgent   session.SetupStepKind = "coder_agent"
	stepToken   session.SetupStepKind = "coder_keepalive_token"
	stepConnect session.SetupStepKind = "coder_connect"
	stepLink    session.SetupStepKind = "coder_link_workspace"
	stepSession session.SetupStepKind = "coder_create_session"
)

var createStages = []session.SessionSetupStep{
	{Kind: stepBuild, Label: "Build the Coder workspace"},
	{Kind: stepAgent, Label: "Wait for the workspace agent"},
	{Kind: stepToken, Label: "Install the keepalive token"},
	{Kind: stepConnect, Label: "Connect to the workspace"},
	{Kind: stepLink, Label: "Register the repo on the workspace"},
	{Kind: stepSession, Label: "Create the session on the workspace"},
}

type progress struct {
	sessions  Sessions
	sessionID uint
	stages    []session.SessionSetupStep
	follower  []session.SessionSetupStep
	written   []session.SessionSetupStep
}

func newProgress(sessions Sessions, sessionID uint, stages []session.SessionSetupStep) *progress {
	p := &progress{sessions: sessions, sessionID: sessionID, stages: slices.Clone(stages)}
	for i := range p.stages {
		p.stages[i].Status = session.SetupStepPending
	}
	p.flush()
	return p
}

func (p *progress) run(kind session.SetupStepKind, fn func() error) error {
	i := slices.IndexFunc(p.stages, func(st session.SessionSetupStep) bool { return st.Kind == kind })
	p.stages[i].Status = session.SetupStepRunning
	p.flush()
	err := fn()
	if err != nil {
		p.stages[i].Status = session.SetupStepFailed
		p.stages[i].ErrorMsg = err.Error()
	} else {
		p.stages[i].Status = session.SetupStepDone
	}
	p.flush()
	return err
}

func (p *progress) followerSteps(steps []session.SessionSetupStep) {
	sorted := slices.Clone(steps)
	slices.SortStableFunc(sorted, func(a, b session.SessionSetupStep) int { return a.Position - b.Position })
	p.follower = make([]session.SessionSetupStep, len(sorted))
	for i, st := range sorted {
		p.follower[i] = session.SessionSetupStep{Kind: st.Kind, Label: st.Label, Status: st.Status, ErrorMsg: st.ErrorMsg}
	}
	p.flush()
}

func (p *progress) flush() {
	all := append(slices.Clone(p.stages), p.follower...)
	if slices.EqualFunc(all, p.written, sameStep) {
		return
	}
	if err := p.sessions.SetRemoteSteps(p.sessionID, all); err != nil {
		slog.Warn("coder: record setup steps failed", "session", p.sessionID, "error", err)
		return
	}
	p.written = all
}

func sameStep(a, b session.SessionSetupStep) bool {
	return a.Kind == b.Kind && a.Label == b.Label && a.Status == b.Status && a.ErrorMsg == b.ErrorMsg
}
