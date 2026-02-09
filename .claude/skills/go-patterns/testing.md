# Go Testing Patterns

## Afero for Filesystem Tests

Never use `t.TempDir()` or real filesystem. Use `afero.NewMemMapFs()` -- faster, no cleanup, full isolation.

```go
func setupSessionStore(t *testing.T) *SessionStore {
	t.Helper()
	return NewSessionStore(afero.NewMemMapFs(), "/config")
}
```

## Setup Helpers with t.Helper()

Always mark setup functions with `t.Helper()` so test failures report the caller's line, not the helper's.

```go
func setupSessionService(t *testing.T) (*SessionService, *SessionStore, *workspace.WorkspaceStore) {
	t.Helper()

	bus := eventbus.NewEventBus()
	sessionStore := NewSessionStore(afero.NewMemMapFs(), "/config")
	workspaceStore := workspace.NewWorkspaceStore()

	workspaceStore.Add(&workspace.Workspace{ID: "ws-1", Name: "test", Path: "/tmp/test"})

	service := NewSessionService(sessionStore, workspaceStore, bus)
	return service, sessionStore, workspaceStore
}
```

## Test Data Isolation

Each test sets up its own data. Never rely on shared fixtures or OnAppStart data.

```go
func TestListSessions(t *testing.T) {
	service, store, _ := setupSessionService(t)

	store.Add(&Session{ID: "s1", WorkspaceID: "ws-1", LastUsedAt: time.Now()})
	store.Add(&Session{ID: "s2", WorkspaceID: "ws-1", LastUsedAt: time.Now()})

	sessions, err := service.ListSessions(context.Background())
	require.NoError(t, err)
	require.Len(t, sessions, 2)
}
```

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

	session := &Session{ID: "session-1", WorkspaceID: "ws-1", LastUsedAt: time.Now()}
	sessionStore.Add(session)

	req := httptest.NewRequest("GET", "/session-1", nil)
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response SessionResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, "session-1", response.ID)
}
```

## Concurrency Tests

Verify thread safety with goroutines:

```go
func TestSessionStore_ConcurrentAccess(t *testing.T) {
	store := setupSessionStore(t)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			store.Add(&Session{
				ID:         string(rune('a' + id)),
				LastUsedAt: time.Now(),
			})
		}(i)
	}

	wg.Wait()
	require.Len(t, store.List(), 10)
}
```
