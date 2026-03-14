# Store Patterns

Stores use GORM with SQLite for persistence. Each store takes a `db.Database` interface and uses GORM queries for all CRUD operations.

## Store Constructor

Stores accept a `db.Database` interface -- no filesystem, no mutex, no in-memory maps.

From `internal/session/sessionstore.go`:

```go
type SessionStore struct {
	db db.Database
}

func NewSessionStore(database db.Database) *SessionStore {
	return &SessionStore{
		db: database,
	}
}
```

Production: `NewSessionStore(database)` where `database` is opened via `db.OpenSQLite(path)`.
Tests: `NewSessionStore(database)` where `database` is opened via `db.OpenInMemory()`.

## GORM Read Queries

Use `Joins("Workspace")` to LEFT JOIN associated workspace data in a single query. Map `gorm.ErrRecordNotFound` to domain-specific sentinel errors.

```go
func (s *SessionStore) GetByID(id uint) (*Session, error) {
	var session Session
	if err := s.db.Joins("Workspace").First(&session, "sessions.id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return &session, nil
}

func (s *SessionStore) List() []Session {
	var sessions []Session
	s.db.Joins("Workspace").Order("sessions.last_used_at DESC").Find(&sessions)
	return sessions
}

func (s *SessionStore) ListByWorkspace(workspaceID uint) []Session {
	var sessions []Session
	s.db.Joins("Workspace").Where("sessions.workspace_id = ?", workspaceID).Order("sessions.last_used_at DESC").Find(&sessions)
	return sessions
}
```

List returns value types (not pointers) -- GORM already returns copies, no defensive copying needed.

## GORM Write Queries

Use `Omit("Workspace")` on Create/Save to avoid writing the association. Check for unique constraint violations.

```go
func (s *SessionStore) Add(session *Session) error {
	if session == nil {
		return errors.New("session cannot be nil")
	}

	if err := s.db.Omit("Workspace").Create(session).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || isUniqueConstraintError(err) {
			return fmt.Errorf("session '%s' already exists: %w", session.TmuxSessionName, ErrSessionAlreadyExists)
		}
		return err
	}
	return nil
}

func (s *SessionStore) Update(session *Session) error {
	if session == nil {
		return errors.New("session cannot be nil")
	}
	if session.ID == 0 {
		return errors.New("session ID cannot be zero")
	}

	var existing Session
	if err := s.db.First(&existing, "id = ?", session.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSessionNotFound
		}
		return err
	}

	return s.db.Omit("Workspace").Save(session).Error
}

func (s *SessionStore) Delete(id uint) error {
	if id == 0 {
		return errors.New("session ID cannot be zero")
	}

	var existing Session
	if err := s.db.First(&existing, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSessionNotFound
		}
		return err
	}

	return s.db.Delete(&Session{}, "id = ?", id).Error
}
```

## Error Mapping

Map GORM errors to domain sentinel errors. SQLite unique constraint errors need string matching as a fallback:

```go
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
```

## Input Validation

Validate nil and zero-value ID at the top of mutating methods, before hitting the database:

```go
func (s *SessionStore) Add(session *Session) error {
	if session == nil {
		return errors.New("session cannot be nil")
	}
	// ...
}
```

## Lifecycle

Store lifecycle methods are no-ops -- migration is handled by `DatabaseModule.OnAppStart`. Stores just need to satisfy the interface:

```go
func (s *SessionStore) OnAppStart(ctx context.Context) error { return nil }
func (s *SessionStore) OnAppEnd(ctx context.Context) error  { return nil }
```

## Database Interface

From `internal/db/dbservice.go`:

```go
type Database interface {
	Create(value any) *gorm.DB
	Save(value any) *gorm.DB
	Delete(value any, conds ...any) *gorm.DB
	First(dest any, conds ...any) *gorm.DB
	Find(dest any, conds ...any) *gorm.DB
	Where(query any, args ...any) *gorm.DB
	Model(value any) *gorm.DB
	Order(value any) *gorm.DB
	Joins(query string, args ...any) *gorm.DB
	Omit(columns ...string) *gorm.DB
	Migrate(models ...any) error
	Close() error
}
```

The interface returns `*gorm.DB` from query-building methods to allow chaining. It abstracts lifecycle (Open/Close/Migrate) while GORM's fluent API handles query composition.

## Model Registration

Modules that own database models implement `common.ModelProvider`:

```go
func (m *SessionModule) Models() []any {
	return []any{&Session{}}
}
```

The app collects models from all modules and registers them with the database for migration.
