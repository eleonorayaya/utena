package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSessionBlocker_Label(t *testing.T) {
	require.Equal(t, "waiting on sam", SessionBlocker{Kind: BlockerKindReview, Note: "waiting on sam", URL: "https://x"}.Label())
	require.Equal(t, "https://x", SessionBlocker{Kind: BlockerKindReview, URL: "https://x"}.Label())
	require.Equal(t, "slack", SessionBlocker{Kind: BlockerKindSlack}.Label())
}

func TestBlockerKind_Valid(t *testing.T) {
	require.True(t, BlockerKindReview.Valid())
	require.True(t, BlockerKindOther.Valid())
	require.False(t, BlockerKind("email").Valid())
	require.False(t, BlockerKind("").Valid())
}

func TestSession_IsBlocked(t *testing.T) {
	now := time.Now()
	s := &Session{}
	require.False(t, s.IsBlocked())

	s.Blockers = []SessionBlocker{{Kind: BlockerKindReview, ResolvedAt: &now}}
	require.False(t, s.IsBlocked())
	require.Empty(t, s.OpenBlockers())

	s.Blockers = append(s.Blockers, SessionBlocker{Model: gorm.Model{ID: 7}, Kind: BlockerKindSlack})
	require.True(t, s.IsBlocked())
	require.Len(t, s.OpenBlockers(), 1)
	require.Equal(t, uint(7), s.OpenBlockers()[0].ID)
}

func TestSessionBlockerStore_CRUD(t *testing.T) {
	store, ws1ID, _ := setupSessionStore(t)
	swStore := NewSessionWorktreeStore(store.db)
	sess := &Session{Name: "s1", Status: StatusActive}
	addTestSession(t, store, swStore, sess, ws1ID)

	bs := NewSessionBlockerStore(store.db)
	open := &SessionBlocker{SessionID: sess.ID, Kind: BlockerKindReview, Note: "pr"}
	resolved := &SessionBlocker{SessionID: sess.ID, Kind: BlockerKindSlack}
	require.NoError(t, bs.Create(open))
	require.NoError(t, bs.Create(resolved))
	now := time.Now()
	resolved.ResolvedAt = &now
	require.NoError(t, bs.Update(resolved))

	loaded, err := store.GetByID(sess.ID)
	require.NoError(t, err)
	require.Len(t, loaded.Blockers, 2)
	require.Len(t, loaded.OpenBlockers(), 1)
	require.Equal(t, open.ID, loaded.OpenBlockers()[0].ID)

	got, err := bs.GetByID(open.ID)
	require.NoError(t, err)
	require.Equal(t, "pr", got.Note)

	require.NoError(t, bs.Delete(open.ID))
	_, err = bs.GetByID(open.ID)
	require.ErrorIs(t, err, ErrBlockerNotFound)
}

func TestSessionStore_PreloadsBlockers(t *testing.T) {
	store, ws1ID, _ := setupSessionStore(t)
	swStore := NewSessionWorktreeStore(store.db)
	sess := &Session{Name: "s1", Status: StatusActive}
	addTestSession(t, store, swStore, sess, ws1ID)
	require.NoError(t, NewSessionBlockerStore(store.db).Create(&SessionBlocker{SessionID: sess.ID, Kind: BlockerKindOther}))

	loaded, err := store.GetByID(sess.ID)
	require.NoError(t, err)
	require.True(t, loaded.IsBlocked())
	require.Equal(t, StatusActive, loaded.Status)

	list, err := store.List()
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Len(t, list[0].Blockers, 1)

	require.True(t, NewSessionResponse(loaded).IsBlocked)
}

func TestSessionStore_HardDeleteCascadesBlockers(t *testing.T) {
	store, ws1ID, _ := setupSessionStore(t)
	swStore := NewSessionWorktreeStore(store.db)
	sess := &Session{Name: "s1", Status: StatusArchived}
	addTestSession(t, store, swStore, sess, ws1ID)
	bs := NewSessionBlockerStore(store.db)
	require.NoError(t, bs.Create(&SessionBlocker{SessionID: sess.ID, Kind: BlockerKindOther}))

	require.NoError(t, store.HardDelete(sess.ID))
	var remaining []SessionBlocker
	require.NoError(t, store.db.Where("session_id = ?", sess.ID).Find(&remaining).Error)
	require.Empty(t, remaining)
}
