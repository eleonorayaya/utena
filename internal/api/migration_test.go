package api

import (
	"context"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/mux"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type legacyTmuxSession struct {
	gorm.Model
	Name     string `gorm:"uniqueIndex"`
	StartDir string
	Env      map[string]string `gorm:"serializer:json"`
	Status   string            `gorm:"index"`
}

func (legacyTmuxSession) TableName() string { return "tmux_sessions" }

type legacySessionAction struct {
	gorm.Model
	SessionID uint   `gorm:"index;not null"`
	Trigger   string `gorm:"not null"`
	Type      string `gorm:"not null"`
	Options   string `gorm:"type:text"`
	Error     string `gorm:"type:text"`
}

func (legacySessionAction) TableName() string { return "session_actions" }

type legacySession struct {
	gorm.Model
	Name           string
	TodoID         *uint `gorm:"index"`
	Status         string
	IsAttached     bool
	LastUsedAt     time.Time
	SessionRoot    string                `gorm:"index"`
	SessionActions []legacySessionAction `gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"`
	TmuxSessionID  *uint                 `gorm:"uniqueIndex"`
	StatusError    string
	TmuxSession    *legacyTmuxSession `gorm:"foreignKey:TmuxSessionID"`
}

func (legacySession) TableName() string { return "sessions" }

type sqliteObject struct {
	Type string
	Name string
}

func sqliteNames(t *testing.T, gormDB *gorm.DB) map[string]string {
	t.Helper()
	var objs []sqliteObject
	require.NoError(t, gormDB.Raw("SELECT type, name FROM sqlite_master").Scan(&objs).Error)
	names := make(map[string]string, len(objs))
	for _, o := range objs {
		names[o.Name] = o.Type
	}
	return names
}

func tableSQL(t *testing.T, gormDB *gorm.DB, table string) string {
	t.Helper()
	var sql string
	require.NoError(t, gormDB.Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&sql).Error)
	return sql
}

func schemaSnapshot(t *testing.T, gormDB *gorm.DB) []string {
	t.Helper()
	var sqls []string
	require.NoError(t, gormDB.Raw("SELECT type || ' ' || name || ' ' || coalesce(sql, '') FROM sqlite_master ORDER BY name").Scan(&sqls).Error)
	return sqls
}

func rowCount(t *testing.T, gormDB *gorm.DB, table string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, gormDB.Table(table).Unscoped().Count(&n).Error)
	return n
}

func assertNoTmuxSchema(t *testing.T, gormDB *gorm.DB) {
	t.Helper()
	var leftovers []sqliteObject
	require.NoError(t, gormDB.Raw("SELECT type, name FROM sqlite_master WHERE sql LIKE '%tmux%' OR name LIKE '%tmux%'").Scan(&leftovers).Error)
	assert.Empty(t, leftovers)
	if _, ok := sqliteNames(t, gormDB)["sqlite_sequence"]; ok {
		var seqs []string
		require.NoError(t, gormDB.Raw("SELECT name FROM sqlite_sequence WHERE name LIKE '%tmux%'").Scan(&seqs).Error)
		assert.Empty(t, seqs)
	}
}

type indexInfo struct {
	Name   string
	Unique bool
}

func indexesOf(t *testing.T, gormDB *gorm.DB, table string) map[string]bool {
	t.Helper()
	var idx []indexInfo
	require.NoError(t, gormDB.Raw("SELECT name, \"unique\" FROM pragma_index_list(?)", table).Scan(&idx).Error)
	out := make(map[string]bool, len(idx))
	for _, i := range idx {
		out[i.Name] = i.Unique
	}
	return out
}

type fkInfo struct {
	From  string
	Table string
	To    string
}

func foreignKeysOf(t *testing.T, gormDB *gorm.DB, table string) []fkInfo {
	t.Helper()
	var fks []fkInfo
	require.NoError(t, gormDB.Raw("SELECT \"from\", \"table\", \"to\" FROM pragma_foreign_key_list(?)", table).Scan(&fks).Error)
	return fks
}

func TestMigration_LegacyTmuxSchema(t *testing.T) {
	ctx := context.Background()
	gormDB, err := db.OpenInMemorySQLite()
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&legacyTmuxSession{}, &legacySession{}, &legacySessionAction{}))

	tmuxRow := &legacyTmuxSession{Name: "work", StartDir: "/src/work", Env: map[string]string{"UTENA_SESSION_ID": "1"}, Status: "active"}
	require.NoError(t, gormDB.Create(tmuxRow).Error)
	todoID := uint(42)
	lastUsed := time.Date(2026, 10, 1, 12, 30, 0, 0, time.UTC)
	sessRow := &legacySession{
		Name:        "s1",
		TodoID:      &todoID,
		Status:      "active",
		IsAttached:  true,
		LastUsedAt:  lastUsed,
		SessionRoot: "/sessions/s1",
		StatusError: "previous failure",
		SessionActions: []legacySessionAction{
			{Trigger: "on_create", Type: "claude", Options: `{"prompt":"hi"}`},
		},
		TmuxSessionID: &tmuxRow.ID,
	}
	require.NoError(t, gormDB.Create(sessRow).Error)

	before := sqliteNames(t, gormDB)
	for _, name := range []string{"tmux_sessions", "idx_tmux_sessions_name", "idx_tmux_sessions_deleted_at", "idx_tmux_sessions_status", "idx_sessions_tmux_session_id"} {
		assert.Contains(t, before, name)
	}
	assert.Contains(t, tableSQL(t, gormDB, "sessions"), "fk_sessions_tmux_session")
	assert.Contains(t, tableSQL(t, gormDB, "session_actions"), "fk_sessions_session_actions")

	cfg := Config{ConfigDir: "/config", SessionsRoot: t.TempDir()}
	app := newApp(gormDB, afero.NewMemMapFs(), cfg)
	require.NoError(t, app.DB.OnAppStart(ctx))

	sess, err := app.Session.Store.GetByID(sessRow.ID)
	require.NoError(t, err)
	require.NotNil(t, sess.MuxSession)
	require.NotNil(t, sess.MuxSessionID)
	assert.Equal(t, tmuxRow.ID, *sess.MuxSessionID)
	assert.Equal(t, "work", sess.MuxSession.Name)
	assert.Equal(t, "/src/work", sess.MuxSession.StartDir)
	assert.Equal(t, mux.StatusActive, sess.MuxSession.Status)
	assert.Equal(t, map[string]string{"UTENA_SESSION_ID": "1"}, sess.MuxSession.Env)
	assert.Equal(t, "s1", sess.Name)
	require.NotNil(t, sess.TodoID)
	assert.Equal(t, todoID, *sess.TodoID)
	assert.Equal(t, "active", string(sess.Status))
	assert.True(t, sess.IsAttached)
	assert.True(t, lastUsed.Equal(sess.LastUsedAt))
	assert.Equal(t, "/sessions/s1", sess.SessionRoot)
	assert.Equal(t, "previous failure", sess.StatusError)
	require.Len(t, sess.SessionActions, 1)
	assert.Equal(t, `{"prompt":"hi"}`, sess.SessionActions[0].Options)

	assertNoTmuxSchema(t, gormDB)

	assert.Equal(t, []fkInfo{{From: "mux_session_id", Table: "mux_sessions", To: "id"}}, foreignKeysOf(t, gormDB, "sessions"))
	assert.Equal(t, []fkInfo{{From: "session_id", Table: "sessions", To: "id"}}, foreignKeysOf(t, gormDB, "session_actions"))
	assert.Contains(t, tableSQL(t, gormDB, "sessions"), "fk_sessions_mux_session")

	var fkViolations []map[string]any
	require.NoError(t, gormDB.Raw("PRAGMA foreign_key_check").Scan(&fkViolations).Error)
	assert.Empty(t, fkViolations)

	sessIdx := indexesOf(t, gormDB, "sessions")
	assert.Equal(t, map[string]bool{
		"idx_sessions_mux_session_id": true,
		"idx_sessions_todo_id":        false,
		"idx_sessions_session_root":   false,
		"idx_sessions_deleted_at":     false,
	}, sessIdx)
	muxIdx := indexesOf(t, gormDB, "mux_sessions")
	assert.Equal(t, map[string]bool{
		"idx_mux_sessions_name":       true,
		"idx_mux_sessions_status":     false,
		"idx_mux_sessions_deleted_at": false,
	}, muxIdx)

	schema := schemaSnapshot(t, gormDB)
	counts := map[string]int64{}
	for _, table := range []string{"sessions", "mux_sessions", "session_actions"} {
		counts[table] = rowCount(t, gormDB, table)
	}
	assert.Equal(t, map[string]int64{"sessions": 1, "mux_sessions": 1, "session_actions": 1}, counts)

	require.NoError(t, app.DB.OnAppStart(ctx))
	assert.Equal(t, schema, schemaSnapshot(t, gormDB))
	for table, n := range counts {
		assert.Equal(t, n, rowCount(t, gormDB, table), table)
	}
}

func TestMigration_FreshDB(t *testing.T) {
	ctx := context.Background()
	gormDB, err := db.OpenInMemorySQLite()
	require.NoError(t, err)

	cfg := Config{ConfigDir: "/config", SessionsRoot: t.TempDir()}
	app := newApp(gormDB, afero.NewMemMapFs(), cfg)
	require.NoError(t, app.DB.OnAppStart(ctx))

	assert.Contains(t, sqliteNames(t, gormDB), "mux_sessions")
	assertNoTmuxSchema(t, gormDB)
	assert.Equal(t, []fkInfo{{From: "mux_session_id", Table: "mux_sessions", To: "id"}}, foreignKeysOf(t, gormDB, "sessions"))

	schema := schemaSnapshot(t, gormDB)
	require.NoError(t, app.DB.OnAppStart(ctx))
	assert.Equal(t, schema, schemaSnapshot(t, gormDB))
}

func TestMigration_HealsPartiallyRenamedSchema(t *testing.T) {
	ctx := context.Background()
	gormDB, err := db.OpenInMemorySQLite()
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&legacyTmuxSession{}, &legacySession{}, &legacySessionAction{}))
	tmuxRow := &legacyTmuxSession{Name: "work", Status: "active"}
	require.NoError(t, gormDB.Create(tmuxRow).Error)
	require.NoError(t, gormDB.Create(&legacySession{Name: "s1", TmuxSessionID: &tmuxRow.ID}).Error)
	require.NoError(t, gormDB.Migrator().RenameTable("tmux_sessions", "mux_sessions"))
	require.NoError(t, gormDB.Migrator().RenameColumn("sessions", "tmux_session_id", "mux_session_id"))

	cfg := Config{ConfigDir: "/config", SessionsRoot: t.TempDir()}
	app := newApp(gormDB, afero.NewMemMapFs(), cfg)
	require.NoError(t, app.DB.OnAppStart(ctx))

	assertNoTmuxSchema(t, gormDB)
	assert.Equal(t, []fkInfo{{From: "mux_session_id", Table: "mux_sessions", To: "id"}}, foreignKeysOf(t, gormDB, "sessions"))
	sess, err := app.Session.Store.GetByID(1)
	require.NoError(t, err)
	require.NotNil(t, sess.MuxSession)
	assert.Equal(t, "work", sess.MuxSession.Name)
}

func TestMigration_RefusesWhenBothTablesExist(t *testing.T) {
	gormDB, err := db.OpenInMemorySQLite()
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&legacyTmuxSession{}, &mux.Session{}))

	cfg := Config{ConfigDir: "/config", SessionsRoot: t.TempDir()}
	app := newApp(gormDB, afero.NewMemMapFs(), cfg)
	err = app.DB.OnAppStart(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mux:")
	assert.Contains(t, sqliteNames(t, gormDB), "tmux_sessions")
}
