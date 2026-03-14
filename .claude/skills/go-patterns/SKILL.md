---
name: go-patterns
description: Use when the user asks to implement, modify, or add Go code involving data stores, HTTP handlers, services, error handling, module wiring, cross-module communication, or tests in a layered Go architecture.
---

# Go Patterns

Patterns for building layered Go services with testable stores, typed errors, event-driven communication, and chi HTTP routing -- learned from production use in this codebase.

## When to Use

- Implementing or modifying a GORM-backed data store (CRUD, queries, associations)
- Adding HTTP endpoints with chi router
- Designing error handling (choosing between sentinel vs custom error types)
- Wiring modules together or adding cross-module communication
- Writing tests for any Go component (stores, services, controllers, routers)
- Creating a new domain package from scratch

## Reference Files

| File | When to Load |
|------|-------------|
| `store-patterns.md` | Implementing or modifying any GORM-backed store -- covers Database interface, queries, associations, error mapping, model registration |
| `error-handling.md` | Designing errors or handling them in controllers -- covers sentinel vs custom types, errors.As, HTTP error responses |
| `testing.md` | Writing any tests -- covers in-memory SQLite setup, setup helpers, table tests, HTTP handler tests, testing error types |
| `event-bus.md` | When one module needs to trigger behavior in another without direct coupling |
| `chi-routing.md` | Setting up HTTP routes, middleware, request binding, or mounting sub-routers |
| `module-structure.md` | Creating a new domain package or wiring components together -- covers layer separation and lifecycle |

## Core Principles

- Accept interfaces, return structs. Inject `db.Database` and other dependencies via constructors.
- Stores use GORM queries against SQLite. No in-memory maps, no mutexes, no defensive copying needed.
- Map `gorm.ErrRecordNotFound` to domain sentinel errors. Use `Joins("Workspace")` for association loading, `Omit("Workspace")` on writes.
- Use `afero.Fs` only for non-database file I/O (e.g., workspace config.json). Tests use `db.OpenInMemory()` for store tests.
- Choose sentinel errors (`errors.Is`) for simple cases, custom error types (`errors.As`) when the error needs to carry context like an ID.
- Structure each domain as store/service/controller/router/module layers, wired together by the module constructor. Modules implement `ModelProvider` to register GORM models.
- Do not add comments to code. The code should be self-documenting through clear naming.
