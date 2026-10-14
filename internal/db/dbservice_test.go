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

	database.RegisterPreMigrations(func(tx *gorm.DB) error {
		return tx.Migrator().RenameTable("legacy_widgets", "widgets")
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

	database.RegisterPreMigrations(func(tx *gorm.DB) error {
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
		func(tx *gorm.DB) error {
			return tx.Migrator().RenameTable("legacy_widgets", "widgets")
		},
		func(tx *gorm.DB) error {
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
		func(tx *gorm.DB) error {
			calls = append(calls, "first")
			return nil
		},
		func(tx *gorm.DB) error {
			calls = append(calls, "second")
			return sentinel
		},
		func(tx *gorm.DB) error {
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

	database.RegisterPreMigrations(func(tx *gorm.DB) error {
		if err := tx.Migrator().RenameColumn(&widget{}, "legacy_name", "label"); err != nil {
			return err
		}
		return tx.Migrator().DropColumn(&widget{}, "obsolete")
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

	database.RegisterPreMigrations(func(tx *gorm.DB) error {
		return tx.Migrator().RenameTable("legacy_parents", "parents")
	})

	require.NoError(t, database.OnAppStart(context.Background()))

	var fks []struct {
		Table string
	}
	require.NoError(t, database.Raw("SELECT \"table\" FROM pragma_foreign_key_list('children')").Scan(&fks).Error)
	require.Len(t, fks, 1)
	assert.Equal(t, "parents", fks[0].Table)
}

func TestDB_PreMigrationExecRolledBackOnLaterFailure(t *testing.T) {
	database := openTestDB(t)
	require.NoError(t, database.Exec("CREATE TABLE widgets (id integer primary key, name text)").Error)
	require.NoError(t, database.Exec("INSERT INTO widgets (id, name) VALUES (1, 'old')").Error)
	sentinel := errors.New("boom")

	database.RegisterPreMigrations(
		func(tx *gorm.DB) error {
			return tx.Exec("UPDATE widgets SET name = 'new' WHERE name = 'old'").Error
		},
		func(tx *gorm.DB) error {
			return sentinel
		},
	)

	require.ErrorIs(t, database.OnAppStart(context.Background()), sentinel)
	var name string
	require.NoError(t, database.Raw("SELECT name FROM widgets WHERE id = 1").Scan(&name).Error)
	assert.Equal(t, "old", name)
}

func TestDB_MigratePreservesAutoincrementAcrossTableRebuild(t *testing.T) {
	database := openTestDB(t)
	require.NoError(t, database.Exec("CREATE TABLE `widgets` (`id` integer PRIMARY KEY AUTOINCREMENT, `name` text, `obsolete` text)").Error)
	for _, name := range []string{"a", "b", "c"} {
		require.NoError(t, database.Exec("INSERT INTO widgets (name) VALUES (?)", name).Error)
	}
	require.NoError(t, database.Exec("DELETE FROM widgets WHERE id = 3").Error)

	database.RegisterPreMigrations(func(tx *gorm.DB) error {
		return tx.Migrator().DropColumn(&widget{}, "obsolete")
	})
	database.RegisterModels(&widget{})
	require.NoError(t, database.OnAppStart(context.Background()))

	require.False(t, database.Migrator().HasColumn(&widget{}, "obsolete"))
	w := &widget{Name: "d"}
	require.NoError(t, database.Create(w).Error)
	assert.Equal(t, uint(4), w.ID)
}

func TestDB_MigrateRestoresVanishedSequenceRow(t *testing.T) {
	database := openTestDB(t)
	require.NoError(t, database.Exec("CREATE TABLE `widgets` (`id` integer PRIMARY KEY AUTOINCREMENT, `name` text, `obsolete` text)").Error)
	require.NoError(t, database.Exec("INSERT INTO widgets (name) VALUES ('a')").Error)
	require.NoError(t, database.Exec("DELETE FROM widgets").Error)

	database.RegisterPreMigrations(func(tx *gorm.DB) error {
		return tx.Migrator().DropColumn(&widget{}, "obsolete")
	})
	require.NoError(t, database.OnAppStart(context.Background()))

	var seq int64
	require.NoError(t, database.Raw("SELECT seq FROM sqlite_sequence WHERE name = 'widgets'").Scan(&seq).Error)
	assert.Equal(t, int64(1), seq)
}

func TestDB_PreMigrationForeignKeyViolationRollsBack(t *testing.T) {
	database := openTestDB(t)
	require.NoError(t, database.Exec("CREATE TABLE parents (id integer primary key)").Error)
	require.NoError(t, database.Exec("CREATE TABLE children (id integer primary key, parent_id integer REFERENCES parents(id))").Error)
	require.NoError(t, database.Exec("CREATE TABLE marks (id integer primary key)").Error)

	database.RegisterPreMigrations(func(tx *gorm.DB) error {
		if err := tx.Exec("INSERT INTO marks (id) VALUES (1)").Error; err != nil {
			return err
		}
		return tx.Exec("INSERT INTO children (id, parent_id) VALUES (1, 99)").Error
	})

	err := database.OnAppStart(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "foreign key violations")
	var count int64
	require.NoError(t, database.Raw("SELECT count(*) FROM children").Scan(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, database.Raw("SELECT count(*) FROM marks").Scan(&count).Error)
	assert.Zero(t, count)
}

func TestDB_MigrateToleratesPreexistingForeignKeyViolations(t *testing.T) {
	database := openTestDB(t)
	require.NoError(t, database.Exec("PRAGMA foreign_keys=OFF").Error)
	require.NoError(t, database.Exec("CREATE TABLE parents (id integer primary key)").Error)
	require.NoError(t, database.Exec("CREATE TABLE children (id integer primary key, parent_id integer REFERENCES parents(id))").Error)
	require.NoError(t, database.Exec("INSERT INTO children (id, parent_id) VALUES (1, 99)").Error)
	require.NoError(t, database.Exec("PRAGMA foreign_keys=ON").Error)

	database.RegisterPreMigrations(func(tx *gorm.DB) error {
		return tx.Exec("UPDATE children SET parent_id = 99 WHERE id = 1").Error
	})
	database.RegisterModels(&widget{})
	require.NoError(t, database.OnAppStart(context.Background()))

	var count int64
	require.NoError(t, database.Raw("SELECT count(*) FROM children").Scan(&count).Error)
	assert.Equal(t, int64(1), count)
}

type gadget struct {
	ID    uint
	Parts []part `gorm:"foreignKey:GadgetID"`
}

type part struct {
	ID       uint
	GadgetID uint
}

func TestDB_MigrateReportsForeignKeyViolationsAfterAutoMigrate(t *testing.T) {
	database := openTestDB(t)
	require.NoError(t, database.Exec("CREATE TABLE `parts` (`id` integer PRIMARY KEY AUTOINCREMENT, `gadget_id` integer)").Error)
	require.NoError(t, database.Exec("INSERT INTO parts (id, gadget_id) VALUES (1, 99)").Error)

	database.RegisterModels(&gadget{}, &part{})
	err := database.OnAppStart(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "after auto-migrate")
	assert.Contains(t, err.Error(), "parts rowid 1 -> gadgets")
}
