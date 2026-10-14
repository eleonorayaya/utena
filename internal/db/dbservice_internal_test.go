package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewForeignKeyViolations_IgnoresBaseline(t *testing.T) {
	database := OpenTestDB(t)
	MustExec(t, database, "PRAGMA foreign_keys=OFF", ParentsDDL, ChildrenDDL, "INSERT INTO children (id, parent_id) VALUES (1, 99)")

	baseline, err := foreignKeyViolations(database.DB)
	require.NoError(t, err)
	require.Len(t, baseline, 1)

	MustExec(t, database, "INSERT INTO children (id, parent_id) VALUES (2, 98)")
	fresh, err := newForeignKeyViolations(database.DB, baseline)
	require.NoError(t, err)
	require.Len(t, fresh, 1)
	assert.Equal(t, "children rowid 2 -> parents", describeViolations(fresh))
}
