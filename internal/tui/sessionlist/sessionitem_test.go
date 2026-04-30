package sessionlist

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/session"
	"github.com/stretchr/testify/assert"
)

func TestSessionItem_Title_ActiveWithWarning(t *testing.T) {
	item := sessionItem{
		session: session.Session{
			Name:        "my-feature",
			Status:      session.StatusActive,
			StatusError: "branch not pulled: worktree has uncommitted changes",
		},
	}
	assert.Contains(t, item.Title(), "[!]")
	assert.NotContains(t, item.Title(), "(broken)")
}

func TestSessionItem_Title_ActiveNoWarning(t *testing.T) {
	item := sessionItem{
		session: session.Session{
			Name:   "my-feature",
			Status: session.StatusActive,
		},
	}
	assert.NotContains(t, item.Title(), "[!]")
}

func TestSessionItem_Title_Broken(t *testing.T) {
	item := sessionItem{
		session: session.Session{
			Name:        "my-feature",
			Status:      session.StatusBroken,
			StatusError: "worktree or branch is unhealthy",
		},
	}
	assert.Contains(t, item.Title(), "(broken)")
	assert.NotContains(t, item.Title(), "[!]")
}
