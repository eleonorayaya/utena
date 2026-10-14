package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckNewForeignKeyViolations_FailsUnderPreexistingConstraint(t *testing.T) {
	database, err := OpenInMemory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	require.NoError(t, database.Exec("CREATE TABLE parents (id integer primary key)").Error)
	require.NoError(t, database.Exec("CREATE TABLE children (id integer primary key, parent_id integer REFERENCES parents(id))").Error)
	known, err := foreignKeyDefs(database.DB)
	require.NoError(t, err)
	assert.True(t, known[foreignKeyDef{Table: "children", From: "parent_id", Parent: "parents"}])

	require.NoError(t, database.Exec("PRAGMA foreign_keys=OFF").Error)
	require.NoError(t, database.Exec("INSERT INTO children (id, parent_id) VALUES (1, 99)").Error)

	err = checkNewForeignKeyViolations(database.DB, nil, known)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "children rowid 1 -> parents")

	baseline, err := foreignKeyViolations(database.DB)
	require.NoError(t, err)
	assert.NoError(t, checkNewForeignKeyViolations(database.DB, baseline, known))
	assert.NoError(t, checkNewForeignKeyViolations(database.DB, nil, map[foreignKeyDef]bool{}))
}
