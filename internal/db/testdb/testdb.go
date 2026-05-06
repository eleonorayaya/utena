package testdb

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/db"
)

func New(t *testing.T, models ...any) db.Database {
	t.Helper()
	database, err := db.OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	if len(models) > 0 {
		if err := database.Migrate(models...); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Logf("close database: %v", err)
		}
	})
	return database
}
