# Window Sizing

## Cache Dimensions in the Parent

The parent model must store width and height and propagate to all children:

```go
type App struct {
	width, height int
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.sessionList.SetSize(msg.Width, msg.Height)
		a.newSession.SetSize(msg.Width, msg.Height)
	}
}
```

## Child SetSize Method

Each child model that wraps a size-dependent component exposes a `SetSize` method:

```go
func (m *SessionListModel) SetSize(width, height int) {
	m.list.SetWidth(width)
	m.list.SetHeight(height)
}
```

Note the pointer receiver -- `SetSize` mutates the model in place.

## Initialize Lists at Zero Size

Create `list.Model` with zero dimensions. The first `WindowSizeMsg` (sent automatically by Bubbletea) sets the real size:

```go
l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
```

## Set Size on View Transitions

When creating a new child model during navigation, immediately set its size from cached dimensions:

```go
case switchToNewSessionMsg:
	a.newSession = NewNewSessionModel()
	a.newSession.SetSize(a.width, a.height)
```

Without this, the new model renders at 0x0 until the next window resize event.

## Common Mistake: Forgetting to Propagate

The most frequent bug is handling `WindowSizeMsg` in the parent but not calling `SetSize` on child models. Every child model with a `list.Model`, `viewport.Model`, or other size-dependent bubbles component needs its own `SetSize` call.
