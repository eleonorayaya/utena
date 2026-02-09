---
name: bubbletea-tui
description: Use when building or modifying a Bubbletea TUI application in Go, including adding views, handling keyboard input, using bubbles components (list, textinput, help, key), composing child models, or handling window sizing.
---

# Bubbletea TUI Patterns

Use the `charmbracelet/bubbles` component library instead of reimplementing UI primitives. The most common mistakes are: manual key string matching instead of `key.Binding`, reimplementing list navigation instead of using `bubbles/list`, and forgetting to propagate `WindowSizeMsg` to child models.

## When to Use

- Adding a new view or screen to a Bubbletea app
- Implementing keyboard navigation or key bindings
- Using `bubbles/list` for any list-based UI
- Using `bubbles/textinput` for text entry
- Handling window resizing in a multi-view app
- Wiring up `bubbles/help` to display contextual key bindings
- Routing between multiple views in a single Bubbletea program

## Reference Files

| File | When to Load |
|------|-------------|
| `key-bindings.md` | When adding keyboard shortcuts, defining keymaps, or integrating help display |
| `list-views.md` | When building a list-based view using `bubbles/list` |
| `view-routing.md` | When building a multi-view app with child model composition |
| `window-sizing.md` | When handling terminal resize or making layouts responsive |

## Core Principles

1. **Use `key.NewBinding` and `key.Matches` for all key handling** -- never match on `msg.String()` directly (except `ctrl+c` for quit).
2. **Use `bubbles/list` for any scrollable item list** -- it provides navigation, filtering, help integration, and pagination for free.
3. **Propagate `WindowSizeMsg` to every child model** that has a size-dependent layout.
4. **Child models return `(ChildModel, tea.Cmd)` from Update** -- not `(tea.Model, tea.Cmd)`. The parent owns the concrete type.
5. **View transitions use message types** -- child models emit navigation messages, the parent's Update handles them by switching the active view.
