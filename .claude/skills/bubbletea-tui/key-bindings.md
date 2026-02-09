# Key Bindings

## Define Bindings with `key.NewBinding`

Every key binding uses `key.NewBinding` with both `WithKeys` (what triggers it) and `WithHelp` (what `bubbles/help` displays):

```go
import "github.com/charmbracelet/bubbles/key"

var (
	selectKey     = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select"))
	newSessionKey = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new"))
	backKey       = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
)
```

## Match Keys with `key.Matches`

Always use `key.Matches` in Update, never `msg.String()`:

```go
case tea.KeyMsg:
	switch {
	case key.Matches(msg, selectKey):
		// handle select
	case key.Matches(msg, backKey):
		// handle back
	}
```

The only exception is `ctrl+c` for quit, which is handled as a global in the parent model before dispatching to children.

## Keymaps for `bubbles/help`

To display context-sensitive help, define a keymap struct implementing `help.KeyMap`:

```go
type nameInputKeys struct {
	Confirm key.Binding
	Back    key.Binding
}

func (k nameInputKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Confirm, k.Back}
}

func (k nameInputKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Confirm, k.Back}}
}

var nameInputKeyMap = nameInputKeys{
	Confirm: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "create")),
	Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}
```

Render it in View with `help.Model`:

```go
func (a App) View() string {
	return a.nameInput.View() + "\n\n" + a.help.View(nameInputKeyMap)
}
```

## Adding Custom Keys to `bubbles/list` Help

The `bubbles/list` component has its own help display. Add custom keys via `AdditionalShortHelpKeys`:

```go
l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
l.AdditionalShortHelpKeys = func() []key.Binding {
	return []key.Binding{selectKey, newSessionKey}
}
```

## Disabling Built-in List Keys

When a list view is nested (e.g., a picker that should not quit on `q`), disable the built-in quit binding:

```go
l.KeyMap.Quit.SetEnabled(false)
```
