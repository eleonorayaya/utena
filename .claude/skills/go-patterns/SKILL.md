---
name: go-patterns
description: Use when the user asks to implement, modify, or add Go code involving data stores, HTTP handlers, services, error handling, module wiring, cross-module communication, or tests in a layered Go architecture.
---

# Go Patterns

Patterns for building layered Go services with testable stores, typed errors, event-driven communication, and chi HTTP routing -- learned from production use in this codebase.

## When to Use

- Implementing or modifying an in-memory data store (CRUD, persistence, thread safety)
- Adding HTTP endpoints with chi router
- Designing error handling (choosing between sentinel vs custom error types)
- Wiring modules together or adding cross-module communication
- Writing tests for any Go component (stores, services, controllers, routers)
- Creating a new domain package from scratch

## Reference Files

| File | When to Load |
|------|-------------|
| `store-patterns.md` | Implementing or modifying any in-memory store -- covers defensive copying, afero filesystem, thread safety, persistence |
| `error-handling.md` | Designing errors or handling them in controllers -- covers sentinel vs custom types, errors.As, HTTP error responses |
| `testing.md` | Writing any tests -- covers afero mocking, setup helpers, table tests, HTTP handler tests, testing error types |
| `event-bus.md` | When one module needs to trigger behavior in another without direct coupling |
| `chi-routing.md` | Setting up HTTP routes, middleware, request binding, or mounting sub-routers |
| `module-structure.md` | Creating a new domain package or wiring components together -- covers layer separation and lifecycle |

## Core Principles

- Accept interfaces, return structs. Inject all external dependencies (filesystem, event bus, other stores) via constructors.
- Stores defensively copy data on both read and write to prevent callers from mutating internal state.
- Use `afero.Fs` for any file I/O so tests can use `afero.NewMemMapFs()` instead of temp directories.
- Choose sentinel errors (`errors.Is`) for simple cases, custom error types (`errors.As`) when the error needs to carry context like an ID.
- Structure each domain as store/service/controller/router/module layers, wired together by the module constructor.
- Do not add comments to code. The code should be self-documenting through clear naming.
