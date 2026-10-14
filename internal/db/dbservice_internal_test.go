package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openInternalTestDB(t *testing.T) *DB {
	t.Helper()
	database, err := OpenInMemory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestRequireForeignKeysOff(t *testing.T) {
	database := openInternalTestDB(t)

	require.NoError(t, database.Exec("PRAGMA foreign_keys=ON").Error)
	err := requireForeignKeysOff(database.DB)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "foreign keys must be off")

	require.NoError(t, database.Exec("PRAGMA foreign_keys=OFF").Error)
	assert.NoError(t, requireForeignKeysOff(database.DB))
}

func TestNewForeignKeyViolations_IgnoresBaseline(t *testing.T) {
	database := openInternalTestDB(t)
	require.NoError(t, database.Exec("PRAGMA foreign_keys=OFF").Error)
	require.NoError(t, database.Exec("CREATE TABLE parents (id integer primary key)").Error)
	require.NoError(t, database.Exec("CREATE TABLE children (id integer primary key, parent_id integer REFERENCES parents(id))").Error)
	require.NoError(t, database.Exec("INSERT INTO children (id, parent_id) VALUES (1, 99)").Error)

	baseline, err := foreignKeyViolations(database.DB)
	require.NoError(t, err)
	require.Len(t, baseline, 1)

	require.NoError(t, database.Exec("INSERT INTO children (id, parent_id) VALUES (2, 98)").Error)
	fresh, err := newForeignKeyViolations(database.DB, baseline)
	require.NoError(t, err)
	require.Len(t, fresh, 1)
	assert.Equal(t, "children rowid 2 -> parents", describeViolations(fresh))
}
