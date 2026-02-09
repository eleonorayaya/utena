# Store Patterns

Every in-memory store must: defensively copy on read and write, use afero for filesystem, protect with sync.RWMutex, and persist after mutations.

## Defensive Copying (Critical)

Without this, callers mutate internal store state. Copy on BOTH read and write.

From `internal/session/sessionstore.go`:

```go
func (s *SessionStore) Add(session *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[session.ID]; exists {
		return fmt.Errorf("session '%s' already exists: %w", session.ID, ErrSessionAlreadyExists)
	}

	copy := *session
	s.sessions[session.ID] = &copy
	return nil
}

func (s *SessionStore) GetByID(id string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[id]
	if !ok {
		return nil, errors.New("session not found")
	}

	copy := *session
	return &copy, nil
}

func (s *SessionStore) List() []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessions := make([]Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, *session)
	}
	return sessions
}
```

List returns value types (not pointers) for the same reason -- the caller gets copies.

## Afero Filesystem Injection

Never use `os.ReadFile`/`os.WriteFile` directly. Inject `afero.Fs` so tests use `afero.NewMemMapFs()`.

```go
type SessionStore struct {
	mu        sync.RWMutex
	sessions  map[string]*Session
	fs        afero.Fs
	configDir string
}

func NewSessionStore(fs afero.Fs, configDir string) *SessionStore {
	return &SessionStore{
		sessions:  make(map[string]*Session),
		fs:        fs,
		configDir: configDir,
	}
}
```

Production: `NewSessionStore(afero.NewOsFs(), configDir)`
Tests: `NewSessionStore(afero.NewMemMapFs(), "/config")`

## Persistence

Load on startup via `OnAppStart`, save after every mutation. Use `afero.ReadFile`/`afero.WriteFile`.

```go
func (s *SessionStore) OnAppStart(ctx context.Context) error {
	data, err := afero.ReadFile(s.fs, s.sessionsPath())
	if err != nil {
		return nil
	}

	loaded := make(map[string]*Session)
	if err := json.Unmarshal(data, &loaded); err != nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = loaded
	return nil
}

func (s *SessionStore) save() {
	s.fs.MkdirAll(s.configDir, 0755)
	data, err := json.Marshal(s.sessions)
	if err != nil {
		return
	}
	afero.WriteFile(s.fs, s.sessionsPath(), data, 0644)
}
```

Call `s.save()` at the end of `Add`, `Update`, and `Delete` -- while the lock is still held.

## Thread Safety

- `sync.RWMutex` for read-heavy workloads
- `RLock()` for GetByID, List, ListByWorkspace
- `Lock()` for Add, Update, Delete
- Always `defer s.mu.Unlock()` / `defer s.mu.RUnlock()` immediately after locking

## Input Validation

Validate nil and empty ID at the top of mutating methods, before acquiring the lock:

```go
func (s *SessionStore) Add(session *Session) error {
	if session == nil {
		return errors.New("session cannot be nil")
	}
	if session.ID == "" {
		return errors.New("session ID cannot be empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// ...
}
```
