package db

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenSQLite_CreatesMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "nested", "utena.db")

	gormDB, err := OpenSQLite(path)
	require.NoError(t, err)
	sqlDB, err := gormDB.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, gormDB.Exec("CREATE TABLE t (id INTEGER)").Error)
	require.FileExists(t, path)
}
