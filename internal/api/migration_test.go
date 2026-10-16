package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/mux"
	"github.com/eleonorayaya/utena/internal/session"
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

type legacySessionSetupStep struct {
	gorm.Model
	SessionID   uint   `gorm:"index;not null"`
	Position    int    `gorm:"not null"`
	Kind        string `gorm:"not null"`
	WorkspaceID *uint  `gorm:"index"`
	DependsOnID *uint  `gorm:"index"`
	Label       string `gorm:"not null"`
	Status      string `gorm:"not null;default:'pending'"`
	ErrorMsg    string `gorm:"type:text"`
}

func (legacySessionSetupStep) TableName() string { return "session_setup_steps" }

type legacySession struct {
	gorm.Model
	TmuxSessionName string `gorm:"uniqueIndex"`
	Name            string
	TodoID          *uint `gorm:"index"`
	Status          string
	IsAttached      bool
	LastUsedAt      time.Time
	SessionRoot     string                    `gorm:"index"`
	SessionActions  []legacySessionAction     `gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"`
	SetupSteps      []legacySessionSetupStep  `gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"`
	ClaudeSessions  []claude.ClaudeSession    `gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"`
	Worktrees       []session.SessionWorktree `gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"`
	Blockers        []session.SessionBlocker  `gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"`
	Branches        []session.SessionBranch   `gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"`
	TmuxSessionID   *uint                     `gorm:"uniqueIndex"`
	StatusError     string
	TmuxSession     *legacyTmuxSession `gorm:"foreignKey:TmuxSessionID"`
}

func (legacySession) TableName() string { return "sessions" }

type sqliteObject struct {
	Type string
	Name string
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
	if gormDB.Migrator().HasTable("sqlite_sequence") {
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

var legacyModels = []any{
	&legacyTmuxSession{}, &legacySession{}, &legacySessionAction{}, &legacySessionSetupStep{},
	&git.Repo{}, &git.Branch{}, &git.Worktree{},
	&claude.ClaudeSession{}, &session.SessionWorktree{}, &session.SessionBlocker{}, &session.SessionBranch{},
}

var sessionChildTables = []string{"session_actions", "session_setup_steps", "claude_sessions", "session_worktrees", "session_blockers", "session_branches"}

func openFileSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	gormDB, err := db.OpenSQLite(filepath.Join(t.TempDir(), "utena.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := gormDB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return gormDB
}

func newMigrationApp(t *testing.T, gormDB *gorm.DB) *App {
	t.Helper()
	return newApp(gormDB, afero.NewMemMapFs(), Config{ConfigDir: "/config", SessionsRoot: t.TempDir(), Browser: "chrome"})
}

func migrateLegacy(t *testing.T, gormDB *gorm.DB) *App {
	t.Helper()
	app := newMigrationApp(t, gormDB)
	require.NoError(t, app.DB.OnAppStart(context.Background()))
	return app
}

func seedLegacySession(t *testing.T, gormDB *gorm.DB) {
	t.Helper()
	require.NoError(t, gormDB.AutoMigrate(legacyModels...))
	tmuxRow := &legacyTmuxSession{Name: "work", Status: "active"}
	require.NoError(t, gormDB.Create(tmuxRow).Error)
	require.NoError(t, gormDB.Create(&legacySession{Name: "s1", TmuxSessionID: &tmuxRow.ID}).Error)
}

func TestMigration_LegacyTmuxSchema_InMemory(t *testing.T) {
	gormDB, err := db.OpenInMemorySQLite()
	require.NoError(t, err)
	runLegacyMigration(t, gormDB)
}

func TestMigration_LegacyTmuxSchema_File(t *testing.T) {
	gormDB := openFileSQLite(t)
	var mode string
	require.NoError(t, gormDB.Raw("PRAGMA journal_mode").Scan(&mode).Error)
	require.Equal(t, "wal", mode)
	runLegacyMigration(t, gormDB)
}

func runLegacyMigration(t *testing.T, gormDB *gorm.DB) {
	require.NoError(t, gormDB.AutoMigrate(legacyModels...))

	repo := &git.Repo{Path: "/src/repo", FullName: "test/repo"}
	require.NoError(t, gormDB.Create(repo).Error)
	branch := &git.Branch{Name: "eqt/s1", RepoID: repo.ID}
	require.NoError(t, gormDB.Create(branch).Error)
	worktree := &git.Worktree{Path: "/sessions/s1/repo", BranchID: branch.ID, RepoID: repo.ID}
	require.NoError(t, gormDB.Create(worktree).Error)

	tmuxRow := &legacyTmuxSession{Name: "work", StartDir: "/src/work", Env: map[string]string{"UTENA_SESSION_ID": "1"}, Status: "active"}
	require.NoError(t, gormDB.Create(tmuxRow).Error)
	todoID := uint(42)
	lastUsed := time.Date(2026, 10, 1, 12, 30, 0, 0, time.UTC)
	sessRow := &legacySession{
		TmuxSessionName: "work",
		Name:            "s1",
		TodoID:          &todoID,
		Status:          "active",
		IsAttached:      true,
		LastUsedAt:      lastUsed,
		SessionRoot:     "/sessions/s1",
		StatusError:     "previous failure",
		SessionActions: []legacySessionAction{
			{Trigger: "on_create", Type: "claude", Options: `{"prompt":"hi"}`},
		},
		SetupSteps: []legacySessionSetupStep{
			{Position: 0, Kind: "apply_claude", Label: "Apply Claude settings", Status: "done"},
			{Position: 1, Kind: "setup_tmux", Label: "Setup tmux", Status: "done"},
		},
		ClaudeSessions: []claude.ClaudeSession{{ClaudeSessionID: "claude-1", Status: claude.StatusIdle, CWD: "/sessions/s1"}},
		Worktrees:      []session.SessionWorktree{{WorktreeID: worktree.ID, Position: 0}},
		Blockers:       []session.SessionBlocker{{Kind: session.BlockerKindReview, Note: "waiting"}},
		Branches:       []session.SessionBranch{{BranchID: branch.ID, FirstSeenAt: lastUsed}},
		TmuxSessionID:  &tmuxRow.ID,
	}
	require.NoError(t, gormDB.Create(sessRow).Error)
	require.NoError(t, gormDB.Create(&legacySession{TmuxSessionName: "s2", Name: "s2"}).Error)
	deleted := &legacySession{TmuxSessionName: "s3", Name: "s3"}
	require.NoError(t, gormDB.Create(deleted).Error)
	require.Equal(t, uint(3), deleted.ID)
	require.NoError(t, gormDB.Unscoped().Delete(deleted).Error)

	require.True(t, gormDB.Migrator().HasTable("tmux_sessions"))
	for _, idx := range []string{"idx_tmux_sessions_name", "idx_tmux_sessions_deleted_at", "idx_tmux_sessions_status"} {
		assert.True(t, gormDB.Migrator().HasIndex("tmux_sessions", idx), idx)
	}
	for _, idx := range []string{"idx_sessions_tmux_session_id", "idx_sessions_tmux_session_name"} {
		assert.True(t, gormDB.Migrator().HasIndex("sessions", idx), idx)
	}
	assert.Contains(t, tableSQL(t, gormDB, "sessions"), "fk_sessions_tmux_session")
	assert.Contains(t, tableSQL(t, gormDB, "session_actions"), "fk_sessions_session_actions")

	app := migrateLegacy(t, gormDB)

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
	assert.Equal(t, "active", string(sess.Status))
	assert.True(t, sess.IsAttached)
	assert.True(t, lastUsed.Equal(sess.LastUsedAt))
	assert.Equal(t, "/sessions/s1", sess.SessionRoot)
	assert.Equal(t, "previous failure", sess.StatusError)
	require.Len(t, sess.ClaudeSessions, 1)
	assert.Equal(t, "claude-1", sess.ClaudeSessions[0].ClaudeSessionID)
	require.Len(t, sess.Worktrees, 1)
	assert.Equal(t, worktree.ID, sess.Worktrees[0].WorktreeID)
	require.Len(t, sess.Blockers, 1)
	assert.Equal(t, "waiting", sess.Blockers[0].Note)
	require.Len(t, sess.Branches, 1)
	assert.Equal(t, branch.ID, sess.Branches[0].BranchID)
	require.Len(t, sess.SessionActions, 1)
	assert.Equal(t, `{"prompt":"hi"}`, sess.SessionActions[0].Options)
	require.Len(t, sess.SetupSteps, 2)
	assert.Equal(t, session.StepKindApplyClaude, sess.SetupSteps[0].Kind)
	assert.Equal(t, "Apply Claude settings", sess.SetupSteps[0].Label)
	assert.Equal(t, session.StepKindSetupMux, sess.SetupSteps[1].Kind)
	assert.Equal(t, "Setup terminal session", sess.SetupSteps[1].Label)
	assert.Equal(t, session.SetupStepDone, sess.SetupSteps[1].Status)
	assert.True(t, gormDB.Migrator().HasColumn("sessions", "mux_session_id"))

	assertNoTmuxSchema(t, gormDB)

	assert.Equal(t, []fkInfo{{From: "mux_session_id", Table: "mux_sessions", To: "id"}}, foreignKeysOf(t, gormDB, "sessions"))
	for _, child := range sessionChildTables {
		assert.Contains(t, foreignKeysOf(t, gormDB, child), fkInfo{From: "session_id", Table: "sessions", To: "id"}, child)
	}
	assert.Contains(t, tableSQL(t, gormDB, "sessions"), "fk_sessions_mux_session")

	var fkViolations []map[string]any
	require.NoError(t, gormDB.Raw("PRAGMA foreign_key_check").Scan(&fkViolations).Error)
	assert.Empty(t, fkViolations)

	sessIdx := indexesOf(t, gormDB, "sessions")
	assert.Equal(t, map[string]bool{
		"idx_sessions_mux_session_id": true,
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
	for _, table := range append([]string{"sessions", "mux_sessions"}, sessionChildTables...) {
		counts[table] = rowCount(t, gormDB, table)
	}
	assert.Equal(t, map[string]int64{
		"sessions":            2,
		"mux_sessions":        1,
		"session_actions":     1,
		"session_setup_steps": 2,
		"claude_sessions":     1,
		"session_worktrees":   1,
		"session_blockers":    1,
		"session_branches":    1,
	}, counts)

	require.NoError(t, app.DB.OnAppStart(context.Background()))
	assert.Equal(t, schema, schemaSnapshot(t, gormDB))
	for table, n := range counts {
		assert.Equal(t, n, rowCount(t, gormDB, table), table)
	}

	fresh := &session.Session{Name: "s4", Status: session.StatusActive}
	require.NoError(t, app.Session.Store.Add(fresh))
	assert.Equal(t, uint(4), fresh.ID)
}

func TestMigration_FreshDB(t *testing.T) {
	gormDB, err := db.OpenInMemorySQLite()
	require.NoError(t, err)

	app := migrateLegacy(t, gormDB)

	assert.True(t, gormDB.Migrator().HasTable("mux_sessions"))
	assertNoTmuxSchema(t, gormDB)
	assert.Equal(t, []fkInfo{{From: "mux_session_id", Table: "mux_sessions", To: "id"}}, foreignKeysOf(t, gormDB, "sessions"))

	schema := schemaSnapshot(t, gormDB)
	require.NoError(t, app.DB.OnAppStart(context.Background()))
	assert.Equal(t, schema, schemaSnapshot(t, gormDB))
}

func TestMigration_HealsPartiallyRenamedSchema(t *testing.T) {
	gormDB, err := db.OpenInMemorySQLite()
	require.NoError(t, err)
	seedLegacySession(t, gormDB)
	require.NoError(t, gormDB.Migrator().RenameTable("tmux_sessions", "mux_sessions"))
	require.NoError(t, gormDB.Migrator().RenameColumn("sessions", "tmux_session_id", "mux_session_id"))

	app := migrateLegacy(t, gormDB)

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
	require.NoError(t, gormDB.Create(&legacyTmuxSession{Name: "stray"}).Error)

	err = newMigrationApp(t, gormDB).DB.OnAppStart(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mux:")
	assert.True(t, gormDB.Migrator().HasTable("tmux_sessions"))
}

func TestMigration_RefusesWhenBothSessionColumnsExist(t *testing.T) {
	gormDB, err := db.OpenInMemorySQLite()
	require.NoError(t, err)
	seedLegacySession(t, gormDB)
	require.NoError(t, gormDB.Exec("ALTER TABLE `sessions` ADD COLUMN `mux_session_id` integer").Error)

	err = newMigrationApp(t, gormDB).DB.OnAppStart(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session:")
	assert.Contains(t, err.Error(), "both tmux_session_id and mux_session_id")
	assert.True(t, gormDB.Migrator().HasTable("tmux_sessions"))
	assert.False(t, gormDB.Migrator().HasTable("mux_sessions"))
	assert.Contains(t, tableSQL(t, gormDB, "sessions"), "fk_sessions_tmux_session")
}

func TestMigration_DropsEmptyLeftoversFromOldBinary(t *testing.T) {
	gormDB := openFileSQLite(t)
	seedLegacySession(t, gormDB)
	migrateLegacy(t, gormDB)

	require.NoError(t, gormDB.AutoMigrate(legacyModels...))
	require.True(t, gormDB.Migrator().HasTable("tmux_sessions"))
	require.True(t, gormDB.Migrator().HasColumn("sessions", "tmux_session_id"))

	app := migrateLegacy(t, gormDB)

	assertNoTmuxSchema(t, gormDB)
	assert.Equal(t, []fkInfo{{From: "mux_session_id", Table: "mux_sessions", To: "id"}}, foreignKeysOf(t, gormDB, "sessions"))
	sess, err := app.Session.Store.GetByID(1)
	require.NoError(t, err)
	require.NotNil(t, sess.MuxSession)
	assert.Equal(t, "work", sess.MuxSession.Name)

	var fkViolations []map[string]any
	require.NoError(t, gormDB.Raw("PRAGMA foreign_key_check").Scan(&fkViolations).Error)
	assert.Empty(t, fkViolations)
}
