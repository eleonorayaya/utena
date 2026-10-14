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

func TestDB_PreMigrationFailureRollsBackEarlierPreMigrations(t *testing.T) {
	database := openTestDB(t)
	require.NoError(t, database.Exec("CREATE TABLE legacy_widgets (id integer primary key, name text)").Error)
	sentinel := errors.New("boom")

	database.RegisterPreMigrations(
		func(m gorm.Migrator) error {
			return m.RenameTable("legacy_widgets", "widgets")
		},
		func(m gorm.Migrator) error {
			return sentinel
		},
	)
	database.RegisterModels(&widget{})

	err := database.OnAppStart(context.Background())
	require.ErrorIs(t, err, sentinel)
	assert.True(t, database.Migrator().HasTable("legacy_widgets"))
	assert.False(t, database.Migrator().HasTable("widgets"))
}

func TestDB_PreMigrationsRunInOrderAndStopOnError(t *testing.T) {
	database := openTestDB(t)
	sentinel := errors.New("boom")
	var calls []string

	database.RegisterPreMigrations(
		func(m gorm.Migrator) error {
			calls = append(calls, "first")
			return nil
		},
		func(m gorm.Migrator) error {
			calls = append(calls, "second")
			return sentinel
		},
		func(m gorm.Migrator) error {
			calls = append(calls, "third")
			return nil
		},
	)

	err := database.OnAppStart(context.Background())
	require.ErrorIs(t, err, sentinel)
	assert.Equal(t, []string{"first", "second"}, calls)
}

func TestDB_PreMigrationTableRecreateNestsInTransaction(t *testing.T) {
	database := openTestDB(t)
	require.NoError(t, database.Exec("CREATE TABLE `widgets` (`id` integer primary key, `name` text, `legacy_name` text, `obsolete` text)").Error)
	require.NoError(t, database.Exec("INSERT INTO widgets (id, name, legacy_name, obsolete) VALUES (1, 'gear', 'old', 'x')").Error)

	database.RegisterPreMigrations(func(m gorm.Migrator) error {
		if err := m.RenameColumn(&widget{}, "legacy_name", "label"); err != nil {
			return err
		}
		return m.DropColumn(&widget{}, "obsolete")
	})
	database.RegisterModels(&widget{})

	require.NoError(t, database.OnAppStart(context.Background()))

	assert.True(t, database.Migrator().HasColumn(&widget{}, "label"))
	assert.False(t, database.Migrator().HasColumn(&widget{}, "legacy_name"))
	assert.False(t, database.Migrator().HasColumn(&widget{}, "obsolete"))
	var label string
	require.NoError(t, database.Raw("SELECT label FROM widgets WHERE id = 1").Scan(&label).Error)
	assert.Equal(t, "old", label)
}

func TestDB_PreMigrationRenameUpdatesForeignKeyReferences(t *testing.T) {
	database := openTestDB(t)
	require.NoError(t, database.Exec("CREATE TABLE legacy_parents (id integer primary key)").Error)
	require.NoError(t, database.Exec("CREATE TABLE children (id integer primary key, parent_id integer REFERENCES legacy_parents(id))").Error)

	database.RegisterPreMigrations(func(m gorm.Migrator) error {
		return m.RenameTable("legacy_parents", "parents")
	})

	require.NoError(t, database.OnAppStart(context.Background()))

	var fks []struct {
		Table string
	}
	require.NoError(t, database.Raw("SELECT \"table\" FROM pragma_foreign_key_list('children')").Scan(&fks).Error)
	require.Len(t, fks, 1)
	assert.Equal(t, "parents", fks[0].Table)
}

func TestMigratorDB_ExecutesInsidePreMigrationTransaction(t *testing.T) {
	database := openTestDB(t)
	require.NoError(t, database.Exec("CREATE TABLE widgets (id integer primary key, name text)").Error)
	require.NoError(t, database.Exec("INSERT INTO widgets (id, name) VALUES (1, 'old')").Error)
	sentinel := errors.New("boom")

	database.RegisterPreMigrations(
		func(m gorm.Migrator) error {
			tx, err := db.MigratorDB(m)
			if err != nil {
				return err
			}
			return tx.Exec("UPDATE widgets SET name = 'new' WHERE name = 'old'").Error
		},
		func(m gorm.Migrator) error {
			return sentinel
		},
	)

	require.ErrorIs(t, database.OnAppStart(context.Background()), sentinel)
	var name string
	require.NoError(t, database.Raw("SELECT name FROM widgets WHERE id = 1").Scan(&name).Error)
	assert.Equal(t, "old", name)
}
