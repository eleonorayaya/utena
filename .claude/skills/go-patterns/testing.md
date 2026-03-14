# Go Testing Patterns

## In-Memory SQLite for Store Tests

Use `db.OpenInMemory()` for all store tests. This gives real SQL semantics without filesystem overhead.

```go
func setupTestDB(t *testing.T) db.Database {
	t.Helper()
	database, err := db.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	database.Migrate(&workspace.Workspace{}, &Session{})
	t.Cleanup(func() { database.Close() })
	return database
}
```

## Setup Helpers with t.Helper()

Always mark setup functions with `t.Helper()` so test failures report the caller's line, not the helper's.

```go
func setupSessionStore(t *testing.T) (*SessionStore, uint, uint) {
	t.Helper()
	database := setupTestDB(t)
	ws1 := &workspace.Workspace{Name: "utena", Path: "/tmp/utena"}
	ws2 := &workspace.Workspace{Name: "other", Path: "/tmp/other"}
	database.Create(ws1)
	database.Create(ws2)
	return NewSessionStore(database), ws1.ID, ws2.ID
}
```

Create workspace records first since stores use foreign keys. GORM auto-fills the `ID` field on Create, so capture it from the struct after creation.

## Test Data Isolation

Each test sets up its own database and data. Never rely on shared fixtures.

```go
func TestListSessions(t *testing.T) {
	store, ws1ID, _ := setupSessionStore(t)

	store.Add(&Session{TmuxSessionName: "s1", WorkspaceID: ws1ID, Status: StatusReady, LastUsedAt: time.Now()})
	store.Add(&Session{TmuxSessionName: "s2", WorkspaceID: ws1ID, Status: StatusReady, LastUsedAt: time.Now()})

	sessions := store.List()
	require.Len(t, sessions, 2)
}
```

Note: with `gorm.Model`, don't set the `ID` field -- let GORM auto-increment it.

## Testing Custom Error Types

Use `errors.As` to verify both that the right error type was returned AND that the context is correct:

```go
func TestGetByID_NotFound(t *testing.T) {
	store := NewWorkspaceStore()

	_, err := store.GetByID("nonexistent")
	require.Error(t, err)

	var wsNotFound *WorkspaceNotFoundError
	require.True(t, errors.As(err, &wsNotFound))
	require.Equal(t, "nonexistent", wsNotFound.WorkspaceID)
}
```

## Table-Driven Tests

```go
func TestValidateSessionName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid", "my-session", false},
		{"empty", "", true},
		{"too long", strings.Repeat("a", 51), true},
		{"invalid chars", "my session!", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSessionName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSessionName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}
```

## HTTP Handler Tests

Use `httptest` with the actual chi router to test the full request path:

```go
func TestSessionRouter_GetSessionByID(t *testing.T) {
	router, sessionStore, _ := setupSessionRouter(t)

	session := &Session{TmuxSessionName: "test", WorkspaceID: wsID, Status: StatusReady, LastUsedAt: time.Now()}
	sessionStore.Add(session)

	req := httptest.NewRequest("GET", fmt.Sprintf("/%d", session.ID), nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
}
```

Note: with uint IDs, URL paths use numeric IDs (e.g., `/1`, `/2`), not string slugs.

## Integration Tests with newApp

For full-stack tests, use `newApp` with a mock `TmuxClient` and in-memory SQLite:

```go
func setupTestApp(t *testing.T) (*App, *mockTmuxClient) {
	t.Helper()
	mock := &mockTmuxClient{}
	app := newApp(mock)
	app.OnAppStart(context.Background())
	t.Cleanup(func() { app.OnAppEnd(context.Background()) })
	return app, mock
}
```

## Concurrency Tests

GORM with SQLite handles concurrency via `MaxOpenConns(1)` and `busy_timeout`. Concurrency tests verify that the store layer handles concurrent access correctly:

```go
func TestSessionStore_ConcurrentAccess(t *testing.T) {
	store, ws1ID, _ := setupSessionStore(t)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			store.Add(&Session{
				TmuxSessionName: fmt.Sprintf("session-%d", id),
				WorkspaceID:     ws1ID,
				Status:          StatusReady,
				LastUsedAt:      time.Now(),
			})
		}(i)
	}

	wg.Wait()
	require.Len(t, store.List(), 10)
}
```
