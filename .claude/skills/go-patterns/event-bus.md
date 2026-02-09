# Event Bus Pattern

Use when one module needs to trigger behavior in another. Prefer over direct function injection or cross-module method calls.

## Interface and Implementation

From `internal/eventbus/eventbus.go`:

```go
type Event struct {
	Type string
	Data interface{}
}

type Handler func(ctx context.Context, event Event) error

type EventBus interface {
	Subscribe(eventType string, handler Handler)
	Publish(ctx context.Context, event Event) error
}

type InMemoryEventBus struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
}

func NewEventBus() *InMemoryEventBus {
	return &InMemoryEventBus{
		handlers: make(map[string][]Handler),
	}
}

func (bus *InMemoryEventBus) Subscribe(eventType string, handler Handler) {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	bus.handlers[eventType] = append(bus.handlers[eventType], handler)
}

func (bus *InMemoryEventBus) Publish(ctx context.Context, event Event) error {
	bus.mu.RLock()
	handlers := bus.handlers[event.Type]
	bus.mu.RUnlock()
	for _, handler := range handlers {
		if err := handler(ctx, event); err != nil {
			return err
		}
	}
	return nil
}
```

## Typed Events

From `internal/eventbus/events.go`:

```go
const (
	SessionCreateRequested = "session.create_requested"
	SessionActivated       = "session.activated"
)

type SessionCreateRequestedEvent struct {
	SessionName   string
	WorkspacePath string
}

type SessionActivatedEvent struct {
	SessionName string
}
```

## Publishing (Service Layer)

From `internal/session/sessionservice.go`:

```go
func (s *SessionService) ActivateSession(ctx context.Context, name string) (*Session, error) {
	session, err := s.store.GetByID(name)
	if err != nil {
		return nil, err
	}

	session.LastUsedAt = time.Now()
	session.IsActive = true
	if err := s.store.Update(session); err != nil {
		return nil, err
	}

	s.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.SessionActivated,
		Data: eventbus.SessionActivatedEvent{SessionName: name},
	})
	return session, nil
}
```

## Subscribing (OnAppStart)

```go
func (z *ZellijService) OnAppStart(ctx context.Context) error {
	z.eventBus.Subscribe(eventbus.SessionActivated, z.handleSessionActivated)
	return nil
}

func (z *ZellijService) handleSessionActivated(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.SessionActivatedEvent)
	if !ok {
		return nil
	}
	return z.SwitchSession(data.SessionName)
}
```

## Anti-Pattern: Function Injection

This creates tight coupling and makes testing harder:

```go
// Bad
sessionModule.Controller.SetSwitchSessionFunc(zellijModule.Service.SwitchSession)
```
