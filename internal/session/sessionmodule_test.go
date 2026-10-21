package session

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/jobs"
	"github.com/stretchr/testify/assert"
)

func TestSessionModule_DisableGitHubSkipsPRActivity(t *testing.T) {
	enabled := jobs.NewJobService()
	(&SessionModule{}).RegisterJobs(enabled)
	assert.NoError(t, enabled.TriggerJob("session.pr_activity"))

	module := &SessionModule{}
	module.DisableGitHub()
	disabled := jobs.NewJobService()
	module.RegisterJobs(disabled)
	assert.Error(t, disabled.TriggerJob("session.pr_activity"))
	assert.NoError(t, disabled.TriggerJob("session.branches"))
}
