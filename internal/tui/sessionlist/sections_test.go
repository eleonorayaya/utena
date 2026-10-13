package sessionlist

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func withClaude(id uint, name string, st claude.ClaudeSessionStatus) session.Session {
	return session.Session{
		Model:          gorm.Model{ID: id},
		Name:           name,
		Status:         session.StatusActive,
		ClaudeSessions: []claude.ClaudeSession{{Status: st}},
	}
}

func names(ss []session.Session) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.Name)
	}
	return out
}

func TestSectionOf(t *testing.T) {
	assert.Equal(t, sectionNeedsYou, sectionOf(withClaude(1, "a", claude.StatusNeedsAttention)))
	assert.Equal(t, sectionNeedsYou, sectionOf(withClaude(1, "a", claude.StatusReadyForReview)))
	assert.Equal(t, sectionWorking, sectionOf(withClaude(1, "a", claude.StatusWorking)))
	assert.Equal(t, sectionIdle, sectionOf(withClaude(1, "a", claude.StatusIdle)))
	assert.Equal(t, sectionIdle, sectionOf(withClaude(1, "a", claude.StatusDone)))
	assert.Equal(t, sectionIdle, sectionOf(session.Session{Status: session.StatusActive}))

	blocked := withClaude(1, "a", claude.StatusNeedsAttention)
	blocked.Blockers = []session.SessionBlocker{openBlocker(session.BlockerKindOther)}
	assert.Equal(t, sectionBlocked, sectionOf(blocked))

	hidden := blocked
	hidden.Status = session.StatusArchived
	assert.Equal(t, sectionHidden, sectionOf(hidden))
}

func TestSortSessions_SectionThenRecency(t *testing.T) {
	now := time.Now()
	idleOld := withClaude(1, "idle-old", claude.StatusIdle)
	idleOld.LastUsedAt = now.Add(-2 * time.Hour)
	idleNew := withClaude(2, "idle-new", claude.StatusIdle)
	idleNew.LastUsedAt = now.Add(-time.Minute)
	working := withClaude(3, "working", claude.StatusWorking)
	review := withClaude(4, "review", claude.StatusReadyForReview)
	review.LastUsedAt = now.Add(-time.Hour)
	attention := withClaude(5, "attention", claude.StatusNeedsAttention)
	attention.LastUsedAt = now

	ss := []session.Session{idleOld, working, review, idleNew, attention}
	sortSessions(ss)
	assert.Equal(t, []string{"attention", "review", "working", "idle-new", "idle-old"}, names(ss))
}

func TestSortSessions_AttachedStaysInItsSection(t *testing.T) {
	attached := withClaude(1, "attached", claude.StatusWorking)
	attached.IsAttached = true
	ss := []session.Session{attached, withClaude(2, "needs", claude.StatusNeedsAttention)}
	sortSessions(ss)
	assert.Equal(t, []string{"needs", "attached"}, names(ss))
}

func TestLayoutRows_HeadersGapsAndCounts(t *testing.T) {
	ss := []session.Session{
		withClaude(1, "a", claude.StatusNeedsAttention),
		withClaude(2, "b", claude.StatusReadyForReview),
		withClaude(3, "c", claude.StatusIdle),
	}
	rows := layoutRows(ss)
	kinds := []rowKind{}
	for _, r := range rows {
		kinds = append(kinds, r.kind)
	}
	assert.Equal(t, []rowKind{rowHeader, rowSession, rowSession, rowGap, rowHeader, rowSession}, kinds)
	assert.Equal(t, 2, rows[0].count)
	assert.Equal(t, sectionIdle, rows[4].section)
	assert.Equal(t, 1, rows[4].count)
	assert.Equal(t, 5, rowOfSession(rows, 2))
}

func TestSessionList_RendersSectionHeadersWithCounts(t *testing.T) {
	m, _ := New().Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{
		withClaude(1, "needs", claude.StatusNeedsAttention),
		withClaude(2, "busy", claude.StatusWorking),
	}})
	out := m.View()
	assert.Contains(t, out, "Needs you 1")
	assert.Contains(t, out, "Working 1")
	assert.NotContains(t, out, "Idle")
	assert.Less(t, strings.Index(out, "needs"), strings.Index(out, "Working 1"))
}

func TestSessionList_CursorFollowsSessionAcrossRegroup(t *testing.T) {
	m, _ := New().Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{
		withClaude(1, "a", claude.StatusNeedsAttention),
		withClaude(2, "b", claude.StatusWorking),
	}})
	m, _, _ = m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	require.Equal(t, "b", m.selectedSession().Name)

	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{
		withClaude(1, "a", claude.StatusIdle),
		withClaude(2, "b", claude.StatusNeedsAttention),
	}})
	assert.Equal(t, "b", m.selectedSession().Name)
}

func TestSessionList_TinyHeightNeverOverflowsAndKeepsCursorVisible(t *testing.T) {
	var ss []session.Session
	statuses := []claude.ClaudeSessionStatus{claude.StatusNeedsAttention, claude.StatusWorking, claude.StatusIdle}
	for i := uint(1); i <= 12; i++ {
		ss = append(ss, withClaude(i, "s"+string(rune('a'+i)), statuses[i%3]))
	}
	for _, h := range []int{1, 2, 3, 6} {
		m, _ := New().Update(tea.WindowSizeMsg{Width: 40, Height: h})
		m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: ss})
		for range 11 {
			m, _, _ = m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
			out := m.View()
			assert.LessOrEqual(t, strings.Count(out, "\n")+1, h)
			assert.Contains(t, out, m.selectedSession().Name)
		}
		for range 11 {
			m, _, _ = m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
			assert.Contains(t, m.View(), m.selectedSession().Name)
		}
	}
}

func TestSessionList_EmptyState(t *testing.T) {
	m, _ := New().Update(tea.WindowSizeMsg{Width: 60, Height: 10})
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{})
	assert.Contains(t, m.View(), "No sessions — press n to create one")
}
