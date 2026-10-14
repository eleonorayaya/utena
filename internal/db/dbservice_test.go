package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/eleonorayaya/utena/internal/db"
)

type widget struct {
	ID   uint
	Name string
}

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.OpenInMemory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestDB_PreMigrationsRunBeforeAutoMigrate(t *testing.T) {
	database := openTestDB(t)
	require.NoError(t, database.Exec("CREATE TABLE legacy_widgets (id integer primary key, name text)").Error)
	require.NoError(t, database.Exec("INSERT INTO legacy_widgets (id, name) VALUES (1, 'gear')").Error)

	database.RegisterPreMigrations(func(m gorm.Migrator) error {
		return m.RenameTable("legacy_widgets", "widgets")
	})
	database.RegisterModels(&widget{})

	require.NoError(t, database.OnAppStart(context.Background()))

	var count int64
	require.NoError(t, database.Raw("SELECT count(*) FROM widgets").Scan(&count).Error)
	assert.Equal(t, int64(1), count)
	assert.False(t, database.Migrator().HasTable("legacy_widgets"))
}

func TestDB_PreMigrationErrorAbortsMigrate(t *testing.T) {
	database := openTestDB(t)
	sentinel := errors.New("boom")

	database.RegisterPreMigrations(func(m gorm.Migrator) error {
		return sentinel
	})
	database.RegisterModels(&widget{})

	err := database.OnAppStart(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
	assert.False(t, database.Migrator().HasTable(&widget{}))
}
