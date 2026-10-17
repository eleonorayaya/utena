# Plugin Bundles (Backend) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Let utena sessions carry named bundles of Claude Code plugins and enable exactly those plugins in `<session-root>/.claude/settings.local.json` (rollout step 1 of `docs/plans/2026-10-16-plugin-bundles-design.md`; no TUI).

**Architecture:** A new `internal/bundles` package holds the catalog type (loaded from `daemon.json` into `api.Config`) and serves `GET /bundles`. `Session` gains a JSON-serialized `Bundles` column, defaulted from each workspace's `.utena/config.json` `default_bundles`. `claudesettings.ApplyPlugins` rewrites `enabledPlugins` for catalog-managed plugin ids only; the session service calls it during setup, on `AddWorkspace`, on `ActivateSession`, and from a new `PUT /sessions/{id}/bundles`.

**Tech Stack:** Go, chi + go-chi/render, GORM (SQLite, `serializer:json`), testify.

**Conventions (from `CLAUDE.md`):**
- Run tests with `task test` only — never `go test` directly. To focus, it's fine to read failures from the full run.
- No code comments.
- Errors wrapped with `fmt.Errorf("context: %w", err)`.

**Deviations from the design doc:**
- No session-updated event is published on bundle changes: the eventbus has no such event and the TUI polls. Revisit in the TUI step if needed.
- A malformed `daemon.json` now fails daemon start even when `--browser` is passed (the file is always read, for `bundles`). An absent file is still fine.

---

### Task 1: `bundles` catalog type

**Files:**
- Create: `internal/bundles/catalog.go`
- Test: `internal/bundles/catalog_test.go`

**Step 1: Write the failing test**

```go
package bundles

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func testCatalog() Catalog {
	return Catalog{
		"go":   {Plugins: []string{"go@m", "lsp@m"}},
		"docs": {Plugins: []string{"pdf@m", "lsp@m"}},
	}
}

func TestCatalog_ManagedPlugins(t *testing.T) {
	assert.Equal(t, []string{"go@m", "lsp@m", "pdf@m"}, testCatalog().ManagedPlugins())
	assert.Empty(t, Catalog(nil).ManagedPlugins())
}

func TestCatalog_Resolve(t *testing.T) {
	plugins, unknown := testCatalog().Resolve([]string{"docs", "nope", "go"})
	assert.Equal(t, []string{"go@m", "lsp@m", "pdf@m"}, plugins)
	assert.Equal(t, []string{"nope"}, unknown)

	plugins, unknown = Catalog(nil).Resolve([]string{"go"})
	assert.Empty(t, plugins)
	assert.Equal(t, []string{"go"}, unknown)
}

func TestCatalog_Entries(t *testing.T) {
	assert.Equal(t, []Entry{
		{Name: "docs", Plugins: []string{"pdf@m", "lsp@m"}},
		{Name: "go", Plugins: []string{"go@m", "lsp@m"}},
	}, testCatalog().Entries())
	assert.NotNil(t, Catalog(nil).Entries())
}

func TestUnion(t *testing.T) {
	assert.Equal(t, []string{"go", "docs", "web"}, Union([]string{"go", "docs"}, nil, []string{"docs", "web", "go"}))
	assert.Nil(t, Union())
	assert.Nil(t, Union([]string{}))
}
```

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — `internal/bundles` does not compile (`undefined: Catalog`).

**Step 3: Write minimal implementation**

```go
package bundles

import (
	"maps"
	"slices"
)

type Bundle struct {
	Plugins []string `json:"plugins"`
}

type Catalog map[string]Bundle

type Entry struct {
	Name    string   `json:"name"`
	Plugins []string `json:"plugins"`
}

func (c Catalog) ManagedPlugins() []string {
	set := make(map[string]struct{})
	for _, b := range c {
		for _, p := range b.Plugins {
			set[p] = struct{}{}
		}
	}
	return slices.Sorted(maps.Keys(set))
}

func (c Catalog) Resolve(names []string) ([]string, []string) {
	set := make(map[string]struct{})
	var unknown []string
	for _, name := range names {
		b, ok := c[name]
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		for _, p := range b.Plugins {
			set[p] = struct{}{}
		}
	}
	return slices.Sorted(maps.Keys(set)), unknown
}

func (c Catalog) Entries() []Entry {
	entries := make([]Entry, 0, len(c))
	for _, name := range slices.Sorted(maps.Keys(c)) {
		entries = append(entries, Entry{Name: name, Plugins: c[name].Plugins})
	}
	return entries
}

func Union(lists ...[]string) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, list := range lists {
		for _, v := range list {
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}
```

Note: `slices.Sorted` on an empty iterator returns `nil`; the tests use `assert.Empty` for that case.

**Step 4: Run test to verify it passes**

Run: `task test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/bundles
git commit -m "feat(bundles): plugin bundle catalog type"
```

---

### Task 2: Load the catalog from `daemon.json`

**Files:**
- Modify: `internal/api/config.go` (`Config` struct, the `loadDaemonFile` call near line 60, `daemonFile`)
- Test: `internal/api/config_test.go`

**Step 1: Write the failing test** (append to `config_test.go`; add `"github.com/eleonorayaya/utena/internal/bundles"` to imports)

```go
func TestLoadConfig_BundlesFromDaemonFile(t *testing.T) {
	clearWMEnv(t)
	dir := t.TempDir()
	t.Setenv("UTENA_DATA_DIR", dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "daemon.json"), []byte(`{"bundles":{"go":{"plugins":["go@m"]}}}`), 0o644))

	cfg, err := LoadConfig([]string{"--browser", "chrome"})
	require.NoError(t, err)
	assert.Equal(t, bundles.Catalog{"go": {Plugins: []string{"go@m"}}}, cfg.Bundles)
}

func TestLoadConfig_NoDaemonFileMeansNoBundles(t *testing.T) {
	clearWMEnv(t)

	cfg, err := LoadConfig(nil)
	require.NoError(t, err)
	assert.Empty(t, cfg.Bundles)
}
```

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — `cfg.Bundles undefined`.

**Step 3: Write minimal implementation**

In `Config` add the field:

```go
	Bundles          bundles.Catalog
```

Replace the block

```go
	if !browserFlag && os.Getenv("UTENA_BROWSER") == "" {
		file, err := loadDaemonFile(cfg.ConfigDir)
		if err != nil {
			return Config{}, err
		}
		if file.Browser != "" {
			cfg.Browser = file.Browser
		}
	}
```

with

```go
	file, err := loadDaemonFile(cfg.ConfigDir)
	if err != nil {
		return Config{}, err
	}
	cfg.Bundles = file.Bundles
	if !browserFlag && os.Getenv("UTENA_BROWSER") == "" && file.Browser != "" {
		cfg.Browser = file.Browser
	}
```

and extend `daemonFile`:

```go
type daemonFile struct {
	Browser string          `json:"browser"`
	Bundles bundles.Catalog `json:"bundles"`
}
```

Add the `bundles` import.

**Step 4: Run test to verify it passes**

Run: `task test`
Expected: PASS (including the existing `TestLoadConfig_BrowserFromDaemonFile` and `TestLoadConfig_RejectsBadDaemonFile`).

**Step 5: Commit**

```bash
git add internal/api/config.go internal/api/config_test.go
git commit -m "feat(api): load plugin bundles from daemon.json"
```

---

### Task 3: `claudesettings.ApplyPlugins`

**Files:**
- Modify: `internal/claudesettings/claudesettings.go`
- Test: `internal/claudesettings/claudesettings_test.go`

**Step 1: Write the failing test** (append; existing imports cover `encoding/json`, `os`, `path/filepath`, `testing`)

```go
func readSettings(t *testing.T, root string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	return out
}

func writeSettings(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.local.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestApplyPlugins(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		managed  []string
		enabled  []string
		want     map[string]any
	}{
		{
			name:    "creates file with enabled plugins",
			managed: []string{"go@m", "pdf@m"},
			enabled: []string{"go@m"},
			want:    map[string]any{"go@m": true},
		},
		{
			name:     "removes unselected managed plugins",
			existing: `{"enabledPlugins":{"go@m":true,"pdf@m":true}}`,
			managed:  []string{"go@m", "pdf@m"},
			enabled:  []string{"pdf@m"},
			want:     map[string]any{"pdf@m": true},
		},
		{
			name:     "leaves unmanaged plugins alone",
			existing: `{"enabledPlugins":{"mine@m":false,"go@m":true}}`,
			managed:  []string{"go@m"},
			enabled:  nil,
			want:     map[string]any{"mine@m": false},
		},
		{
			name:     "drops the key when nothing remains",
			existing: `{"enabledPlugins":{"go@m":true}}`,
			managed:  []string{"go@m"},
			enabled:  nil,
			want:     nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.existing != "" {
				writeSettings(t, root, tt.existing)
			}
			if err := ApplyPlugins(root, tt.managed, tt.enabled); err != nil {
				t.Fatalf("ApplyPlugins: %v", err)
			}
			got, _ := readSettings(t, root)["enabledPlugins"].(map[string]any)
			if len(got) != len(tt.want) {
				t.Fatalf("enabledPlugins = %v, want %v", got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Fatalf("enabledPlugins = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestApplyPlugins_PreservesSandbox(t *testing.T) {
	root := t.TempDir()
	if err := EnsureSessionRoot(root, []string{"/ws"}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPlugins(root, []string{"go@m"}, []string{"go@m"}); err != nil {
		t.Fatal(err)
	}
	out := readSettings(t, root)
	sandbox, _ := out["sandbox"].(map[string]any)
	fs, _ := sandbox["filesystem"].(map[string]any)
	if allow, _ := fs["allowWrite"].([]any); len(allow) != 2 {
		t.Fatalf("allowWrite = %v, want 2 entries", allow)
	}
}

func TestApplyPlugins_NoFileAndNothingEnabledWritesNothing(t *testing.T) {
	root := t.TempDir()
	if err := ApplyPlugins(root, []string{"go@m"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "settings.local.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no settings file, stat err = %v", err)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — `undefined: ApplyPlugins`.

**Step 3: Write minimal implementation** (add `"bytes"` to imports)

```go
func ApplyPlugins(rootPath string, managed, enabled []string) error {
	settingsPath := filepath.Join(rootPath, ".claude", settingsFileName)
	raw := map[string]any{}
	data, err := os.ReadFile(settingsPath)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parse settings: %w", err)
		}
	case errors.Is(err, os.ErrNotExist):
		if len(enabled) == 0 {
			return nil
		}
	default:
		return fmt.Errorf("read %s: %w", settingsFileName, err)
	}

	plugins, _ := raw["enabledPlugins"].(map[string]any)
	if plugins == nil {
		plugins = make(map[string]any)
	}
	for _, id := range managed {
		delete(plugins, id)
	}
	for _, id := range enabled {
		plugins[id] = true
	}
	if len(plugins) == 0 {
		delete(raw, "enabledPlugins")
	} else {
		raw["enabledPlugins"] = plugins
	}

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	out = append(out, '\n')
	if bytes.Equal(out, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}
	if err := os.WriteFile(settingsPath, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", settingsFileName, err)
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `task test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/claudesettings
git commit -m "feat(claudesettings): manage enabledPlugins for catalog plugins"
```

---

### Task 4: Workspace `default_bundles`

**Files:**
- Modify: `internal/session/workspaceconfig.go`
- Test: `internal/session/workspaceconfig_test.go`

**Step 1: Write the failing test** (append; ensure `os`, `path/filepath`, `require` are imported)

```go
func writeUtenaConfig(t *testing.T, wsPath, body string) {
	t.Helper()
	dir := filepath.Join(wsPath, ".utena")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644))
}

func TestDefaultBundles_UnionsInWorkspaceOrder(t *testing.T) {
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()
	writeUtenaConfig(t, a, `{"default_bundles":["go"]}`)
	writeUtenaConfig(t, b, `{"default_bundles":["docs","go"]}`)

	require.Equal(t, []string{"go", "docs"}, defaultBundles([]string{a, b, c}))
	require.Nil(t, defaultBundles([]string{c}))
}

func TestDefaultBundles_SkipsUnreadableConfig(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	writeUtenaConfig(t, a, `{`)
	writeUtenaConfig(t, b, `{"default_bundles":["go"]}`)

	require.Equal(t, []string{"go"}, defaultBundles([]string{a, b}))
}
```

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — `undefined: defaultBundles`.

**Step 3: Write minimal implementation**

Add to `WorkspaceConfig`:

```go
type WorkspaceConfig struct {
	SetupActions   []WorkspaceSetupAction `json:"setup_actions"`
	DefaultBundles []string               `json:"default_bundles,omitempty"`
}
```

Add (imports: `log/slog`, `github.com/eleonorayaya/utena/internal/bundles`):

```go
func defaultBundles(wsPaths []string) []string {
	lists := make([][]string, 0, len(wsPaths))
	for _, p := range wsPaths {
		cfg, err := loadWorkspaceConfig(p)
		if err != nil {
			slog.Warn("failed to load workspace config", "path", p, "error", err)
			continue
		}
		if cfg != nil {
			lists = append(lists, cfg.DefaultBundles)
		}
	}
	return bundles.Union(lists...)
}
```

**Step 4: Run test to verify it passes**

Run: `task test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/session/workspaceconfig.go internal/session/workspaceconfig_test.go
git commit -m "feat(session): default_bundles in workspace config"
```

---

### Task 5: `Session.Bundles`, catalog on the service, write plugins during setup

**Files:**
- Modify: `internal/session/session.go` (`Session` struct)
- Modify: `internal/session/sessionstore.go` (new `UpdateBundles`)
- Modify: `internal/session/sessionservice.go` (`SessionService` struct, `CreateSessionInput`, `CreateSession`, `applyClaudeSettings`)
- Test: `internal/session/sessionservice_bundles_test.go` (new)

**Step 1: Write the failing test**

```go
package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/bundles"
	"github.com/stretchr/testify/require"
)

func testBundleCatalog() bundles.Catalog {
	return bundles.Catalog{
		"go":   {Plugins: []string{"go@m"}},
		"docs": {Plugins: []string{"docs@m"}},
	}
}

func workspacePath(t *testing.T, service *SessionService, wsID uint) string {
	t.Helper()
	ws, err := service.workspaceService.GetWorkspace(context.Background(), wsID)
	require.NoError(t, err)
	return ws.Path
}

func readEnabledPlugins(t *testing.T, root string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".claude", "settings.local.json"))
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	plugins, _ := raw["enabledPlugins"].(map[string]any)
	return plugins
}

func TestSessionService_CreateSession_DefaultsBundlesFromWorkspaces(t *testing.T) {
	service, sessionStore, _, ws1ID, ws2ID := setupTwoRepoSessionService(t)
	service.SetBundleCatalog(testBundleCatalog())
	writeUtenaConfig(t, workspacePath(t, service, ws1ID), `{"default_bundles":["go"]}`)
	writeUtenaConfig(t, workspacePath(t, service, ws2ID), `{"default_bundles":["go","docs"]}`)

	sess, err := service.CreateSession(context.Background(), CreateSessionInput{
		Name: "bundled",
		Workspaces: []WorkspaceBranchSpec{
			{WorkspaceID: ws1ID, BaseBranch: "main"},
			{WorkspaceID: ws2ID, BaseBranch: "main"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"go", "docs"}, sess.Bundles)

	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)
	require.Equal(t, map[string]any{"go@m": true, "docs@m": true}, readEnabledPlugins(t, sess.SessionRoot))

	stored, err := sessionStore.GetByID(sess.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"go", "docs"}, stored.Bundles)
}

func TestSessionService_CreateSession_ExplicitBundlesOverrideDefaults(t *testing.T) {
	service, _, _, ws1ID, _ := setupTwoRepoSessionService(t)
	service.SetBundleCatalog(testBundleCatalog())
	writeUtenaConfig(t, workspacePath(t, service, ws1ID), `{"default_bundles":["go"]}`)

	sess, err := service.CreateSession(context.Background(), CreateSessionInput{
		Name:       "explicit",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: ws1ID, BaseBranch: "main"}},
		Bundles:    []string{"docs"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"docs"}, sess.Bundles)

	none, err := service.CreateSession(context.Background(), CreateSessionInput{
		Name:       "none",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: ws1ID, BaseBranch: "main"}},
		Bundles:    []string{},
	})
	require.NoError(t, err)
	require.Empty(t, none.Bundles)
}
```

`writeUtenaConfig` comes from Task 4's test file (same package).

If the `.utena/config.json` written into the repo trips the dirty-main-repo check, it only sets `StatusError`; status still reaches `active`. If it doesn't, add `.utena/` to the test repo's `.git/info/exclude` inside `writeUtenaConfig`.

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — `service.SetBundleCatalog undefined`, `sess.Bundles undefined`, `unknown field Bundles in CreateSessionInput`.

**Step 3: Write minimal implementation**

`session.go`, in `Session` after `SessionRoot`:

```go
	Bundles        []string               `json:"bundles,omitempty" gorm:"serializer:json"`
```

`sessionstore.go`:

```go
func (s *SessionStore) UpdateBundles(id uint, names []string) error {
	return s.db.Model(&Session{Model: gorm.Model{ID: id}}).Select("Bundles").Updates(&Session{Bundles: names}).Error
}
```

`sessionservice.go`:

- Add import `github.com/eleonorayaya/utena/internal/bundles`.
- Add field `bundleCatalog bundles.Catalog` to `SessionService`.
- Add `Bundles []string` to `CreateSessionInput`.
- Add:

```go
func (s *SessionService) SetBundleCatalog(c bundles.Catalog) {
	s.bundleCatalog = c
}

func (s *SessionService) writePlugins(ctx context.Context, sess *Session) error {
	enabled, unknown := s.bundleCatalog.Resolve(sess.Bundles)
	if len(unknown) > 0 {
		slog.WarnContext(ctx, "session references unknown bundles", "session", sess.ID, "bundles", unknown)
	}
	return claudesettings.ApplyPlugins(sess.SessionRoot, s.bundleCatalog.ManagedPlugins(), enabled)
}
```

- In `CreateSession`, just before `if err := os.MkdirAll(sessionRoot, 0o755)`:

```go
	sessBundles := input.Bundles
	if sessBundles == nil {
		paths := make([]string, len(slots))
		for i, sl := range slots {
			paths[i] = sl.ws.Path
		}
		sessBundles = defaultBundles(paths)
	}
```

  and set `Bundles: bundles.Union(sessBundles),` in the `&Session{...}` literal.

- In `applyClaudeSettings`, right after the `EnsureSessionRoot` block:

```go
	if err := s.writePlugins(ctx, sess); err != nil {
		slog.WarnContext(ctx, "apply claude plugins failed", "error", err)
		warnings = append(warnings, fmt.Sprintf("claude-plugins: %s", err.Error()))
	}
```

**Step 4: Run test to verify it passes**

Run: `task test`
Expected: PASS (whole suite — the new column must not break existing session tests).

**Step 5: Commit**

```bash
git add internal/session
git commit -m "feat(session): persist bundles and enable their plugins on setup"
```

---

### Task 6: `AddWorkspace` unions in the new workspace's defaults

**Files:**
- Modify: `internal/session/sessionservice.go` (`AddWorkspace`, before `go s.runAddWorkspace`)
- Test: `internal/session/sessionservice_bundles_test.go`

**Step 1: Write the failing test**

```go
func TestSessionService_AddWorkspace_UnionsDefaultBundles(t *testing.T) {
	service, sessionStore, _, ws1ID, ws2ID := setupTwoRepoSessionService(t)
	service.SetBundleCatalog(testBundleCatalog())
	writeUtenaConfig(t, workspacePath(t, service, ws1ID), `{"default_bundles":["go"]}`)
	writeUtenaConfig(t, workspacePath(t, service, ws2ID), `{"default_bundles":["docs"]}`)

	ctx := context.Background()
	sess, err := service.CreateSession(ctx, CreateSessionInput{
		Name:       "grow",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: ws1ID, BaseBranch: "main"}},
	})
	require.NoError(t, err)
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	updated, err := service.AddWorkspace(ctx, sess.ID, WorkspaceBranchSpec{WorkspaceID: ws2ID, BaseBranch: "main"})
	require.NoError(t, err)
	require.Equal(t, []string{"go", "docs"}, updated.Bundles)

	require.Eventually(t, func() bool {
		return readEnabledPlugins(t, sess.SessionRoot)["docs@m"] == true
	}, 10*time.Second, 25*time.Millisecond)
}
```

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — `updated.Bundles` is `[go]`.

**Step 3: Write minimal implementation**

In `AddWorkspace`, immediately before `go s.runAddWorkspace(sess.ID, spec)`:

```go
	if merged := bundles.Union(sess.Bundles, defaultBundles([]string{ws.Path})); len(merged) != len(sess.Bundles) {
		if err := s.store.UpdateBundles(sess.ID, merged); err != nil {
			return nil, fmt.Errorf("persist session bundles: %w", err)
		}
	}
```

`runAddWorkspace` reloads the session and calls `applyClaudeSettings`, which writes the plugins.

**Step 4: Run test to verify it passes**

Run: `task test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/session
git commit -m "feat(session): add workspace default bundles when adding a workspace"
```

---

### Task 7: Reapply plugins on `ActivateSession`

**Files:**
- Modify: `internal/session/sessionservice.go` (`ActivateSession`, after `s.SyncSessionBranches(ctx, session)`)
- Test: `internal/session/sessionservice_bundles_test.go`

**Step 1: Write the failing test**

```go
func TestSessionService_ActivateSession_ReappliesPlugins(t *testing.T) {
	service, sessionStore, _, ws1ID, _ := setupTwoRepoSessionService(t)
	service.SetBundleCatalog(testBundleCatalog())

	ctx := context.Background()
	sess, err := service.CreateSession(ctx, CreateSessionInput{
		Name:       "revive",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: ws1ID, BaseBranch: "main"}},
		Bundles:    []string{"go"},
	})
	require.NoError(t, err)
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	_, err = service.ArchiveSession(ctx, sess.ID)
	require.NoError(t, err)

	service.SetBundleCatalog(bundles.Catalog{"go": {Plugins: []string{"go@m", "gopls@m"}}})
	_, err = service.ActivateSession(ctx, sess.ID)
	require.NoError(t, err)

	require.Equal(t, map[string]any{"go@m": true, "gopls@m": true}, readEnabledPlugins(t, sess.SessionRoot))
}
```

If `ArchiveSession` → `ActivateSession` isn't valid with the fake mux driver, look at `sessionservice_lifecycle_test.go` for the supported path to an activatable inactive session and use that instead.

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — `gopls@m` missing.

**Step 3: Write minimal implementation**

After `s.SyncSessionBranches(ctx, session)` in `ActivateSession`:

```go
	if err := s.writePlugins(ctx, session); err != nil {
		slog.WarnContext(ctx, "apply claude plugins failed", "session", session.ID, "error", err)
	}
```

**Step 4: Run test to verify it passes**

Run: `task test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/session
git commit -m "feat(session): reapply bundle plugins on activate"
```

---

### Task 8: `SetBundles` and the session API

**Files:**
- Modify: `internal/session/sessionservice.go` (new `SetBundles`)
- Modify: `internal/session/types.go` (`CreateSessionRequest.Bundles`, new `SetBundlesRequest`)
- Modify: `internal/session/sessioncontroller.go` (`CreateSession` passes bundles, new `SetBundles`)
- Modify: `internal/session/sessionrouter.go` (route)
- Test: `internal/session/sessionrouter_test.go`

**Step 1: Write the failing test** (append to `sessionrouter_test.go`; add `bundles` import)

```go
func TestSessionRouter_SetBundles(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)
	router.controller.service.SetBundleCatalog(bundles.Catalog{
		"go":   {Plugins: []string{"go@m"}},
		"docs": {Plugins: []string{"docs@m"}},
	})
	swStore := NewSessionWorktreeStore(sessionStore.db)

	session := &Session{
		Name:        "session-1",
		Status:      StatusActive,
		LastUsedAt:  time.Now(),
		SessionRoot: t.TempDir(),
		Bundles:     []string{"go"},
	}
	addTestSession(t, sessionStore, swStore, session, ws1ID)

	req := httptest.NewRequest("PUT", fmt.Sprintf("/%d/bundles", session.ID), bytes.NewReader([]byte(`{"bundles":["docs","docs"]}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.Routes().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	retrieved, err := sessionStore.GetByID(session.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"docs"}, retrieved.Bundles)
	require.Equal(t, map[string]any{"docs@m": true}, readEnabledPlugins(t, session.SessionRoot))
}

func TestSessionRouter_SetBundles_RequiresBundles(t *testing.T) {
	router, _, _, _, _, _ := setupSessionRouter(t)

	req := httptest.NewRequest("PUT", "/1/bundles", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.Routes().ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSessionRouter_SetBundles_NotFound(t *testing.T) {
	router, _, _, _, _, _ := setupSessionRouter(t)

	req := httptest.NewRequest("PUT", "/9999/bundles", bytes.NewReader([]byte(`{"bundles":[]}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.Routes().ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}
```

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — 404/405 from the router for `/{id}/bundles`.

**Step 3: Write minimal implementation**

`sessionservice.go`:

```go
func (s *SessionService) SetBundles(ctx context.Context, id uint, names []string) (*Session, error) {
	sess, err := s.store.GetByID(id)
	if err != nil {
		return nil, err
	}
	if sess.Status == StatusDeleted {
		return nil, common.NewInvalidRequest("cannot set bundles on a deleted session")
	}
	sess.Bundles = bundles.Union(names)
	if err := s.store.UpdateBundles(sess.ID, sess.Bundles); err != nil {
		return nil, fmt.Errorf("persist session bundles: %w", err)
	}
	if err := s.writePlugins(ctx, sess); err != nil {
		return nil, fmt.Errorf("apply claude plugins: %w", err)
	}
	return sess, nil
}
```

`types.go` — add to `CreateSessionRequest`:

```go
	Bundles    []string              `json:"bundles,omitempty"`
```

and:

```go
type SetBundlesRequest struct {
	Bundles []string `json:"bundles"`
}

func (r *SetBundlesRequest) Bind(_ *http.Request) error {
	if r.Bundles == nil {
		return errors.New("bundles is required")
	}
	return nil
}
```

`sessioncontroller.go` — in `CreateSession` add `Bundles: data.Bundles,` to the `CreateSessionInput` literal, and add:

```go
func (c *SessionController) SetBundles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUintParam(r, "id")
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	data := &SetBundlesRequest{}
	if err := render.Bind(r, data); err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	session, err := c.service.SetBundles(ctx, id, data.Bundles)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	common.RenderResponse(w, r, NewSessionResponse(session))
}
```

`sessionrouter.go` — next to the other `/{id}/...` routes:

```go
	r.Put("/{id}/bundles", sr.controller.SetBundles)
```

**Step 4: Run test to verify it passes**

Run: `task test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/session
git commit -m "feat(session): PUT /sessions/{id}/bundles and bundles on create"
```

---

### Task 9: `GET /bundles` and app wiring

**Files:**
- Create: `internal/bundles/module.go`
- Modify: `internal/api/app.go` (`App` struct, `buildApp`, `modules()`)
- Test: `internal/api/bundles_test.go` (new)

**Step 1: Write the failing test**

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBundlesRouteMounted(t *testing.T) {
	_, router, _, _, _ := setupTestRouter(t)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/bundles", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"bundles":[]}`, w.Body.String())
}
```

Also add a unit test in `internal/bundles/catalog_test.go` for a populated catalog (add `net/http`, `net/http/httptest`, `require` imports):

```go
func TestModule_ListsEntries(t *testing.T) {
	m := NewModule(Catalog{"go": {Plugins: []string{"go@m"}}})
	w := httptest.NewRecorder()
	m.Routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"bundles":[{"name":"go","plugins":["go@m"]}]}`, w.Body.String())
}
```

**Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — `undefined: NewModule`; `/bundles` 404.

**Step 3: Write minimal implementation**

`internal/bundles/module.go`:

```go
package bundles

import (
	"context"
	"net/http"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/go-chi/chi/v5"
)

type Module struct {
	catalog Catalog
}

func NewModule(catalog Catalog) *Module {
	return &Module{catalog: catalog}
}

func (m *Module) OnAppStart(context.Context) error { return nil }
func (m *Module) OnAppEnd(context.Context) error   { return nil }

func (m *Module) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", m.list)
	return r
}

type ListResponse struct {
	Bundles []Entry `json:"bundles"`
}

func (*ListResponse) Render(http.ResponseWriter, *http.Request) error { return nil }

func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	common.RenderResponse(w, r, &ListResponse{Bundles: m.catalog.Entries()})
}
```

`internal/api/app.go`:

- Import `github.com/eleonorayaya/utena/internal/bundles`.
- Add `Bundles *bundles.Module` to `App`.
- In `buildApp`, after `sessionModule := ...`:

```go
	sessionModule.Service.SetBundleCatalog(cfg.Bundles)
	bundlesModule := bundles.NewModule(cfg.Bundles)
```

  and set `Bundles: bundlesModule,` in the `&App{...}` literal.
- In `modules()` add `{"bundles", "/bundles", a.Bundles},` after the session entry.

**Step 4: Run test to verify it passes**

Run: `task test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/bundles internal/api
git commit -m "feat(api): GET /bundles and wire catalog into sessions"
```

---

### Task 10: Docs, lint, final check

**Files:**
- Modify: `CLAUDE.md` (Component Communication → mounted controllers list; Go Module Structure)
- Modify: `README.md` (new "Plugin bundles" subsection under Project Setup)

**Step 1:** In `CLAUDE.md`, add `- /bundles - plugin bundle catalog from daemon.json` to the mounted controllers list, mention `PUT /sessions/{id}/bundles` on the `/sessions` line, and add `- internal/bundles/ - plugin bundle catalog (daemon.json "bundles") and GET /bundles` to Go Module Structure.

**Step 2:** In `README.md`, add a short "Plugin bundles" section: the `daemon.json` `bundles` shape, `default_bundles` in a workspace's `.utena/config.json`, that utena writes `enabledPlugins` into `<session-root>/.claude/settings.local.json` (only for plugins in the catalog), and that open Claude panes need `/reload-plugins` after a change.

**Step 3:** Run `task fmt`, `task lint`, `task test`. Expected: clean, all PASS.

**Step 4: Commit**

```bash
git add CLAUDE.md README.md
git commit -m "docs: plugin bundles"
```
