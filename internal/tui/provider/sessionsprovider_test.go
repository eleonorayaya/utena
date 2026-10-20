package provider

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/session"
	"github.com/stretchr/testify/assert"
)

func TestSessionsProvider_PollsWhileCreating(t *testing.T) {
	p := newSessionsProvider(&client{})

	p, _ = p.Update(sessionsLoadedMsg{sessions: []session.Session{{Status: session.StatusCreating}}})
	assert.True(t, p.polling)

	p, cmd := p.Update(pollSessionsTickMsg{})
	assert.NotNil(t, cmd)

	p, _ = p.Update(sessionsLoadedMsg{sessions: []session.Session{{Status: session.StatusActive}}})
	assert.False(t, p.polling)

	_, cmd = p.Update(pollSessionsTickMsg{})
	assert.Nil(t, cmd)
}
