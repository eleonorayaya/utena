package db

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	ParentsDDL  = "CREATE TABLE parents (id integer primary key)"
	ChildrenDDL = "CREATE TABLE children (id integer primary key, parent_id integer REFERENCES parents(id))"
)

func OpenTestDB(t *testing.T) *DB {
	t.Helper()
	database, err := OpenInMemory()
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func MustExec(t *testing.T, database *DB, stmts ...string) {
	t.Helper()
	for _, stmt := range stmts {
		require.NoError(t, database.Exec(stmt).Error, stmt)
	}
}

func CountRows(t *testing.T, database *DB, table string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, database.Raw("SELECT count(*) FROM "+table).Scan(&count).Error)
	return count
}
