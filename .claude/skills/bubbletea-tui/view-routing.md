# View Routing and Child Model Composition

## View Enum

Define views as an `iota` enum:

```go
type view int

const (
	sessionListView view = iota
	workspacePickerView
	nameInputView
)
```

## Parent App Structure

The parent holds all child models as concrete types (not `tea.Model` interface) and tracks the active view:

```go
type App struct {
	activeView    view
	sessionList   SessionListModel
	newSession    NewSessionModel
	nameInput     NameInputModel
	help          help.Model
	width, height int
}
```

## Child Model Signature

Child models return their own concrete type from Update, not `tea.Model`:

```go
func (m SessionListModel) Update(msg tea.Msg) (SessionListModel, tea.Cmd) {
```

## Navigation via Messages

Child models signal navigation by returning command functions that produce navigation messages:

```go
case key.Matches(msg, backKey):
	return m, func() tea.Msg {
		return switchToSessionListMsg{}
	}
```

The parent intercepts these in its own Update before dispatching to children:

```go
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case switchToNewSessionMsg:
		a.activeView = workspacePickerView
		a.newSession = NewNewSessionModel()
		a.newSession.SetSize(a.width, a.height)
		return a, fetchWorkspaces()

	case switchToNameInputMsg:
		a.activeView = nameInputView
		a.nameInput = NewNameInputModel(msg.workspace)
		return a, a.nameInput.Init()

	case switchToSessionListMsg:
		a.activeView = sessionListView
		return a, fetchSessions()
	}

	var cmd tea.Cmd
	switch a.activeView {
	case sessionListView:
		a.sessionList, cmd = a.sessionList.Update(msg)
	case workspacePickerView:
		a.newSession, cmd = a.newSession.Update(msg)
	case nameInputView:
		a.nameInput, cmd = a.nameInput.Update(msg)
	}
	return a, cmd
}
```

## View Dispatch

Route the View call to the active child:

```go
func (a App) View() string {
	switch a.activeView {
	case workspacePickerView:
		return a.newSession.View()
	case nameInputView:
		return a.nameInput.View() + "\n\n" + a.help.View(nameInputKeyMap)
	default:
		return a.sessionList.View()
	}
}
```

## Re-initializing Views on Navigation

When switching to a view, create a fresh model and set its size from the parent's cached dimensions:

```go
case switchToNewSessionMsg:
	a.activeView = workspacePickerView
	a.newSession = NewNewSessionModel()
	a.newSession.SetSize(a.width, a.height)
	return a, fetchWorkspaces()
```

## Global Key Handling

Handle global keys (like `ctrl+c`) in the parent before dispatching to children:

```go
case tea.KeyMsg:
	if msg.String() == "ctrl+c" {
		return a, tea.Quit
	}
```

This is the one place where `msg.String()` is acceptable.
