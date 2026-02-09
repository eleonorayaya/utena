# Reference Tables

## Purpose

Reference tables route Claude to the right file based on the current task. They save context by avoiding loading irrelevant content.

## Format

```markdown
## Reference Files

| File | When to Load |
|------|--------------|
| `testing.md` | When writing tests -- covers afero, table tests, HTTP testing |
| `error-handling.md` | When designing custom errors or choosing errors.Is/As |
```

## Writing Good "When to Load" Descriptions

### Start with the Task, Then List Contents

The description has two parts: when to load it (the task) and what it contains (key topics).

```markdown
# Good -- task + contents
| `testing.md` | When writing tests -- covers afero, table tests, HTTP testing |

# Bad -- just the task
| `testing.md` | When writing tests |

# Bad -- just the contents
| `testing.md` | Afero, table tests, HTTP testing |
```

### Match User Language

Use words the user would actually type when asking for help:

```markdown
# Good -- matches "I need to add an event bus"
| `event-bus.md` | When modules need to communicate without direct coupling |

# Bad -- too abstract
| `event-bus.md` | For pub/sub patterns |
```

### Be Specific Enough to Differentiate

When multiple files exist, descriptions must make it clear which one to load:

```markdown
# Good -- clearly different triggers
| `store-patterns.md` | When implementing in-memory data stores -- covers mutex, defensive copying |
| `service-patterns.md` | When implementing business logic -- covers validation, orchestration |

# Bad -- overlapping, unclear which to pick
| `store-patterns.md` | When working with data |
| `service-patterns.md` | When working with data |
```

## Organizing Reference Files

Three organizational strategies:

**By task type** -- when skills map to workflow stages:
```markdown
| `setup.md` | When starting a new project |
| `testing.md` | When writing tests |
| `debugging.md` | When fixing bugs |
```

**By component** -- when skills map to architectural layers:
```markdown
| `store-patterns.md` | When implementing data stores |
| `service-patterns.md` | When implementing business logic |
| `controller-patterns.md` | When implementing HTTP handlers |
```

**By concept** -- when skills map to cross-cutting concerns:
```markdown
| `error-handling.md` | When designing error types |
| `dependency-injection.md` | When wiring components |
| `event-bus.md` | When decoupling modules |
```

## When to Split vs Combine

Split into separate files when:
- Topics are independently useful (user needs one but not the other)
- A single file would exceed ~200 lines
- Different tasks need different subsets

Combine into one file when:
- Topics are always used together
- Content is short (under ~80 lines total)
- Splitting would lose important context between related patterns
