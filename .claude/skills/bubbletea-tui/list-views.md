# List Views with `bubbles/list`

## Implementing `list.Item`

Every list item must implement `Title()`, `Description()`, and `FilterValue()`:

```go
type sessionItem struct {
	session session.Session
}

func (i sessionItem) Title() string       { return i.session.ID }
func (i sessionItem) Description() string { return i.session.WorkspaceID }
func (i sessionItem) FilterValue() string { return i.session.ID }
```

`FilterValue()` determines what the built-in fuzzy filter searches against. Return the field users would search by.

## Creating a List Model

Wrap `list.Model` in your own struct. Initialize with `list.New` using nil items and zero dimensions (set later via `WindowSizeMsg`):

```go
type SessionListModel struct {
	list list.Model
}

func NewSessionListModel() SessionListModel {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Sessions"
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{selectKey, newSessionKey}
	}
	return SessionListModel{list: l}
}
```

## Loading Items Asynchronously

Items are typically loaded via a `tea.Cmd`. Handle the loaded message by converting to `[]list.Item`:

```go
case sessionsLoadedMsg:
	items := make([]list.Item, len(msg.sessions))
	for i, s := range msg.sessions {
		items[i] = sessionItem{session: s}
	}
	cmd := m.list.SetItems(items)
	return m, cmd
```

Note: `SetItems` returns a `tea.Cmd` -- always return it.

## Handling Keys with Filter State Check

When the list is in filtering mode, your custom key bindings must not fire. Always check `FilterState()` first:

```go
case tea.KeyMsg:
	if m.list.FilterState() == list.Filtering {
		break
	}
	switch {
	case key.Matches(msg, selectKey):
		if item, ok := m.list.SelectedItem().(sessionItem); ok {
			return m, func() tea.Msg {
				return activateSessionMsg{name: item.session.ID}
			}
		}
	case key.Matches(msg, newSessionKey):
		return m, func() tea.Msg { return switchToNewSessionMsg{} }
	}
```

Without the `FilterState` check, pressing `n` while filtering would trigger "new session" instead of typing the letter `n`.

## Always Delegate to the Inner List

After handling your custom keys, always pass the message to the inner `list.Model`:

```go
var cmd tea.Cmd
m.list, cmd = m.list.Update(msg)
return m, cmd
```

This ensures navigation (j/k/up/down), filtering (/), and help (?) all work.

## Getting the Selected Item

Use type assertion on `SelectedItem()`:

```go
if item, ok := m.list.SelectedItem().(sessionItem); ok {
	// use item.session
}
```
