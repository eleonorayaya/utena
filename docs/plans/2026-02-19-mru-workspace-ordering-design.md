# MRU Workspace Ordering

## Problem

Workspaces in the picker are sorted alphabetically. Users want frequently-used workspaces at the top.

## Design

Persist `LastUsedAt` on workspaces. Sort the workspace picker MRU-first, with never-used workspaces alphabetically at the end.

### Data model

Add `LastUsedAt time.Time` to the `Workspace` struct.

### Persistence

Workspace metadata (ID → LastUsedAt) stored in `~/.config/utena/workspace_metadata.json`. On startup, the workspace store discovers workspaces from the filesystem, then merges persisted metadata.

### Update flow

Session service calls `WorkspaceService.Touch(id)` when a session is created or activated. `Touch` sets `LastUsedAt = time.Now()` and calls `store.Update()`.

### Sorting

`WorkspaceStore.List()` returns workspaces with non-zero `LastUsedAt` sorted MRU-first, followed by never-used workspaces sorted alphabetically.

### Layering

Session module interacts with workspaces via `WorkspaceService`, not `WorkspaceStore` directly.

## Changes

1. `internal/workspace/workspace.go` — add `LastUsedAt time.Time`
2. `internal/workspace/workspacestore.go` — add `Update()`, metadata persistence, merge on startup, MRU sort
3. `internal/workspace/workspaceservice.go` — add `Touch(id)`, expose `GetByID()`
4. `internal/session/sessionservice.go` — depend on `*workspace.WorkspaceService`, call `Touch()` in `CreateSession`/`ActivateSession`
5. `internal/session/sessionmodule.go` — pass `workspaceModule.Service` instead of `workspaceModule.Store`
