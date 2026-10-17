# Plugin Bundles TUI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Let users pick plugin bundles in the TUI: a `Bundles` field in the new-session form and a `Bundles` section with an edit key in session detail.

**Architecture:** Rollout step 2 of `docs/plans/2026-10-16-plugin-bundles-design.md`. A small backend endpoint exposes a set of workspaces' `default_bundles` so the form can pre-check them. The TUI gets provider commands for the catalog, the defaults and `PUT /sessions/{id}/bundles`, a reusable `bundlepicker` multi-select overlay, and wires it into `sessionform` and `sessiondetail`.

**Tech Stack:** Go, chi, bubbletea/bubbles (`list`), testify, `internal/tui/uitest`.

**Conventions (from CLAUDE.md):** run everything with `task` from the repo root (`task test`, `task fmt`, `task lint`), never `go` directly. No code comments. Go/Task caching can skip tests; use `GOFLAGS=-count=1 task test --force` when verifying. Commits end with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.

## Deviations from the design

- **Edit key is `p` (plugins), not `b`.** `b` is already "block" in session detail.
- **The form sends `bundles` only once the user edits the field.** Until then it sends nothing and the daemon applies the same workspace defaults it shows. This avoids racing the defaults fetch and keeps one source of truth.
- **New endpoint** `GET /sessions/default-bundles?workspace_id=1&workspace_id=2` → `{"bundles":[...]}`. The design didn't say how the TUI learns workspace defaults; they live in each workspace's `.utena/config.json`, which only the session package reads.

---

### Task 1: `GET /sessions/default-bundles`

**Files:**
- Modify: `internal/session/sessionservice.go` (add method next to `SetBundles`)
- Modify: `internal/session/types.go` (response type)
- Modify: `internal/session/sessioncontroller.go` (handler)
- Modify: `internal/session/sessionrouter.go` (route, before `/{id}` routes; put it next to `/resources`)
- Test: `internal/session/sessionrouter_test.go`

**Step 1: Write failing tests** (append to `sessionrouter_test.go`; helpers `setupTwoRepoSessionService`, `writeWorkspaceConfig`, `workspacePath` already exist)

```go
func getDefaultBundles(t *testing.T, router *SessionRouter, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/default-bundles"+query, nil)
	w := httptest.NewRecorder()
	router.Routes().ServeHTTP(w, req)
	return w
}

func TestSessionRouter_DefaultBundles_UnionsWorkspaces(t *testing.T) {
	service, _, _, ws1ID, ws2ID := setupTwoRepoSessionService(t)
	writeWorkspaceConfig(t, workspacePath(t, service, ws1ID), `{"default_bundles":["go","docs"]}`)
	writeWorkspaceConfig(t, workspacePath(t, service, ws2ID), `{"default_bundles":["docs","web"]}`)
	router := NewSessionRouter(NewSessionController(service))

	w := getDefaultBundles(t, router, fmt.Sprintf("?workspace_id=%d&workspace_id=%d", ws1ID, ws2ID))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp DefaultBundlesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, []string{"go", "docs", "web"}, resp.Bundles)
}

func TestSessionRouter_DefaultBundles_EmptyIsArray(t *testing.T) {
	service, _, _, ws1ID, _ := setupTwoRepoSessionService(t)
	router := NewSessionRouter(NewSessionController(service))

	w := getDefaultBundles(t, router, fmt.Sprintf("?workspace_id=%d", ws1ID))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, `{"bundles":[]}`, w.Body.String())
}

func TestSessionRouter_DefaultBundles_RejectsBadID(t *testing.T) {
	service, _, _, _, _ := setupTwoRepoSessionService(t)
	router := NewSessionRouter(NewSessionController(service))

	w := getDefaultBundles(t, router, "?workspace_id=abc")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestSessionRouter_DefaultBundles_UnknownWorkspace(t *testing.T) {
	service, _, _, _, _ := setupTwoRepoSessionService(t)
	router := NewSessionRouter(NewSessionController(service))

	w := getDefaultBundles(t, router, "?workspace_id=99999")
	require.NotEqual(t, http.StatusOK, w.Code)
}
```

**Step 2:** `GOFLAGS=-count=1 task test --force` → compile failure (`DefaultBundlesResponse` undefined).

**Step 3: Implement**

`types.go`:

```go
type DefaultBundlesResponse struct {
	Bundles []string `json:"bundles"`
}

func (*DefaultBundlesResponse) Render(http.ResponseWriter, *http.Request) error { return nil }
```

`sessionservice.go`:

```go
func (s *SessionService) DefaultBundlesFor(ctx context.Context, workspaceIDs []uint) ([]string, error) {
	paths := make([]string, 0, len(workspaceIDs))
	for _, id := range workspaceIDs {
		ws, err := s.workspaceService.GetWorkspace(ctx, id)
		if err != nil {
			return nil, err
		}
		paths = append(paths, ws.Path)
	}
	return defaultBundles(paths), nil
}
```

`sessioncontroller.go`:

```go
func (c *SessionController) DefaultBundles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var ids []uint
	for _, raw := range r.URL.Query()["workspace_id"] {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			common.RenderError(w, r, common.NewInvalidRequest(fmt.Sprintf("invalid workspace_id %q", raw)))
			return
		}
		ids = append(ids, uint(id))
	}

	names, err := c.service.DefaultBundlesFor(ctx, ids)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}
	if names == nil {
		names = []string{}
	}
	common.RenderResponse(w, r, &DefaultBundlesResponse{Bundles: names})
}
```

(Add `fmt` to the imports if it isn't there.) Router: `r.Get("/default-bundles", sr.controller.DefaultBundles)` right after `/resources`.

**Step 4:** `GOFLAGS=-count=1 task test --force` → PASS.

**Step 5:** Commit `feat(session): GET /sessions/default-bundles`.

---

### Task 2: Provider commands and client calls

**Files:**
- Create: `internal/tui/provider/bundles.go` (public commands + msgs)
- Modify: `internal/tui/provider/client.go` (`fetchBundles`, `fetchDefaultBundles`, `setSessionBundles`; `createSession` gains `bundles []string`)
- Modify: `internal/tui/provider/sessionsprovider.go` (`CreateSession` signature, `createSessionIntentMsg.bundles`, routing of the new intents)
- Modify: `internal/tui/sessionform/sessionform.go:406` (pass `nil` for now so it compiles)
- Test: `internal/tui/provider/client_test.go`

**Messages and commands** (`bundles.go`):

```go
package provider

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/bundles"
)

type BundlesLoadedMsg struct {
	Bundles []bundles.Entry
	Err     error
}

type DefaultBundlesLoadedMsg struct {
	WorkspaceIDs []uint
	Bundles      []string
	Err          error
}

type SessionBundlesSetMsg struct {
	SessionID uint
}

type fetchBundlesIntentMsg struct{}

type fetchDefaultBundlesIntentMsg struct{ workspaceIDs []uint }

type setSessionBundlesIntentMsg struct {
	sessionID uint
	bundles   []string
}

type sessionBundlesSetMsg struct{ sessionID uint }

func FetchBundles() tea.Cmd {
	return func() tea.Msg { return fetchBundlesIntentMsg{} }
}

func FetchDefaultBundles(workspaceIDs []uint) tea.Cmd {
	return func() tea.Msg { return fetchDefaultBundlesIntentMsg{workspaceIDs: workspaceIDs} }
}

func SetSessionBundles(sessionID uint, names []string) tea.Cmd {
	return func() tea.Msg { return setSessionBundlesIntentMsg{sessionID: sessionID, bundles: names} }
}
```

**Client** (follow the existing style in `client.go`: log + `ErrMsg` on transport errors, `parseAPIError` on bad status). **Exception:** the two read calls must fail quietly. An `ErrMsg` would show a red status and reset the form's `submitting` guard, and a TUI talking to an older daemon would error on every workspace pick. On any failure they `log.Printf("[ERROR] ...")` and return the loaded msg with `Err` set:

- `fetchBundles()` → `GET /bundles`, decode `bundles.ListResponse`, return `BundlesLoadedMsg{Bundles: resp.Bundles}`; on failure `BundlesLoadedMsg{Err: err}`.
- `fetchDefaultBundles(ids []uint)` → `GET /sessions/default-bundles?workspace_id=..` (build with `url.Values`, `Add` per id), decode `session.DefaultBundlesResponse`, return `DefaultBundlesLoadedMsg{WorkspaceIDs: ids, Bundles: resp.Bundles}`; on failure `DefaultBundlesLoadedMsg{WorkspaceIDs: ids, Err: err}`.
- `setSessionBundles(id uint, names []string)` → `PUT /sessions/{id}/bundles` with `{"bundles": names}`. Marshal a non-nil slice (`if names == nil { names = []string{} }`) because the server returns 400 for a missing field. Expect 200, return `sessionBundlesSetMsg{sessionID: id}`.
- `createSession(name, specs, selected []string)`: when `selected != nil`, set `body["bundles"] = selected`. (Never name a local or parameter `bundles`; it shadows the `bundles` package. Use `names`/`selected`.) A nil slice omits the key (daemon applies workspace defaults); an empty non-nil slice sends `[]`.

**sessionsprovider.go:**

- `CreateSession(name string, specs []SessionWorkspaceSpec, bundles []string)`; carry `bundles` on `createSessionIntentMsg`.
- In `Update`:

```go
	case fetchBundlesIntentMsg:
		return p, p.client.fetchBundles()

	case fetchDefaultBundlesIntentMsg:
		return p, p.client.fetchDefaultBundles(msg.workspaceIDs)

	case setSessionBundlesIntentMsg:
		return p, p.client.setSessionBundles(msg.sessionID, msg.bundles)

	case sessionBundlesSetMsg:
		id := msg.sessionID
		return p, tea.Batch(
			p.client.fetchSessions(),
			func() tea.Msg { return SessionBundlesSetMsg{SessionID: id} },
		)
```

**Step 1: Write failing tests** (`client_test.go`). Use an `httptest` server that records the request:

```go
func recordingServer(t *testing.T, status int, body string, got *http.Request, gotBody *[]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = *r.Clone(r.Context())
		*gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}
```

Tests:
- `TestFetchBundles_DecodesCatalog`: server returns `{"bundles":[{"name":"go","plugins":["go@m"]}]}` → `BundlesLoadedMsg{Bundles: []bundles.Entry{{Name: "go", Plugins: []string{"go@m"}}}}`; request path `/bundles`.
- `TestFetchDefaultBundles_SendsWorkspaceIDs`: returns `{"bundles":["go"]}`; assert `got.URL.Query()["workspace_id"] == []string{"1","2"}`, path `/sessions/default-bundles`, msg `DefaultBundlesLoadedMsg{WorkspaceIDs: []uint{1,2}, Bundles: []string{"go"}}`.
- `TestSetSessionBundles_PutsBundles`: status 200 `{}`; method PUT, path `/sessions/7/bundles`, body JSONEq `{"bundles":["go"]}`, msg `sessionBundlesSetMsg{sessionID: 7}`.
- `TestSetSessionBundles_NilSendsEmptyArray`: body JSONEq `{"bundles":[]}`.
- `TestSetSessionBundles_ErrorStatus`: status 400 `{"error":"nope"}` → `ErrMsg` whose error text is `nope`.
- `TestFetchBundles_FailureIsQuiet`: status 404 → `BundlesLoadedMsg` with `Err != nil` (not `ErrMsg`).
- `TestFetchDefaultBundles_FailureIsQuiet`: status 404 → `DefaultBundlesLoadedMsg{WorkspaceIDs: ids}` with `Err != nil`.
- `TestCreateSession_OmitsBundlesWhenNil` / `TestCreateSession_SendsEmptyBundles` / `TestCreateSession_SendsBundles`: status 202 `{"id":3}`; decode body into `map[string]any` and check the `bundles` key is absent / `[]any{}` / `[]any{"go"}`.

**Step 2:** run tests → compile failures.

**Step 3:** implement as above; update the one caller in `sessionform.go` to `provider.CreateSession(name, buildSpecs(m.rows, isNew), nil)`.

**Step 4:** `GOFLAGS=-count=1 task test --force` → PASS.

**Step 5:** Commit `feat(tui): provider calls for plugin bundles`.

---

### Task 3: `bundlepicker` overlay

A multi-select list of catalog bundles, modeled on `internal/tui/workspacepicker` (read it first: `workspacepicker.go`, `workspaceitem.go`, `keys.go`, `messages.go`, and its test).

**Files:**
- Create: `internal/tui/bundlepicker/bundlepicker.go`, `item.go`, `keys.go`, `messages.go`
- Test: `internal/tui/bundlepicker/bundlepicker_test.go`

**Behavior:**

- `New(title string) Model`; `SetSize(w, h)`; `CapturesText()` → `ui.FilterTyping(m.list)`.
- `Open(catalog []bundles.Entry, selected []string) Model`: clones `selected`, rebuilds items, selects row 0. Items are every catalog entry in catalog order, then any selected name **not** in the catalog appended as an "unknown" item so the user can uncheck it.
- Space toggles the cursor item (`Keys.Toggle`, `" "`). Enter (`Keys.Select`) emits `SelectedMsg{Bundles: ...}` with the checked names in item order. **Enter with nothing checked emits `SelectedMsg{Bundles: []string{}}` (non-nil, empty)**, which means "clear". Esc (when the filter isn't active) emits `ClosedMsg{}`.
- `Keys()` returns `keyMap{Select, Toggle, Filter, Back}` like workspacepicker.
- Row cells: checkbox (`[ ]` muted / `[x]` primary, width 4 as in workspaceitem), name (`ui.NameStyle(selected)`), then plugins joined with `, ` in muted style. Unknown items show `unknown bundle` in `theme.Current.Error` instead of plugins. `FilterValue()` is the name.
- `View()`: `ui.FormTitle(title)` plus muted `  N selected` when any are checked, a blank line, then `ui.ListView(m.list)`; with an empty catalog and nothing selected show muted `  No bundles configured — add "bundles" to daemon.json`. Wrap in `ui.FitHeight(..., m.height)` and `ui.Clamp` the title like workspacepicker.

`messages.go`:

```go
package bundlepicker

type SelectedMsg struct {
	Bundles []string
}

type ClosedMsg struct{}
```

**Step 1: Write failing tests:**

```go
func catalog() []bundles.Entry {
	return []bundles.Entry{
		{Name: "docs", Plugins: []string{"pdf@m"}},
		{Name: "go", Plugins: []string{"go@m", "gopls@m"}},
	}
}

func opened(selected ...string) Model {
	m := New("Bundles")
	m.SetSize(60, 10)
	return m.Open(catalog(), selected)
}

func space() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")} }
```

- `TestPicker_ShowsBundlesWithPlugins`: view contains `[ ] docs`, `[ ] go`, `go@m, gopls@m`; every line ≤ 60 wide (`lipgloss.Width`).
- `TestPicker_PreChecksSelected`: `opened("go")` view contains `[x] go` and `1 selected`.
- `TestPicker_ToggleAndConfirm`: space on row 0, down, space, enter → `SelectedMsg{Bundles: []string{"docs", "go"}}` (drain the cmd).
- `TestPicker_UntoggleEverythingConfirmsEmpty`: `opened("docs")`, space, enter → msg `Bundles` is non-nil and empty (`assert.NotNil` + `assert.Empty`).
- `TestPicker_UnknownSelectedNameIsListed`: `opened("legacy")` view contains `[x] legacy` and `unknown bundle`; space on it (it's the 3rd row: down, down, space), enter → `Bundles` empty.
- `TestPicker_EscCloses`: esc → `ClosedMsg{}`.
- `TestPicker_EmptyCatalog`: `New("Bundles").Open(nil, nil)` view contains `No bundles configured`.

**Step 2:** run → fail. **Step 3:** implement. **Step 4:** run → PASS.

**Step 5:** Commit `feat(tui): bundle picker overlay`.

---

### Task 4: Bundles field in the new-session form

**Files:**
- Modify: `internal/tui/sessionform/sessionform.go`
- Test: `internal/tui/sessionform/sessionform_test.go`

**Changes:**

- Fields become `fieldWorkspaces, fieldBundles, fieldBranches, fieldMode, fieldName`; `labels` gets `"Bundles"` in second place.
- New overlay `overlayBundles`; model fields `bundlePicker bundlepicker.Model`, `catalog []bundles.Entry`, `selectedBundles []string`, `defaultsLoaded bool`, `bundlesEdited bool` (avoid a field or local named `bundles`; it shadows the package). Below, "`m.bundles`" means `m.selectedBundles`. Construct with `bundlepicker.New("Bundles")` and size it in `SetSize`.
- `Init`: reset `bundles = nil`, `bundlesEdited = false`, keep `catalog` (don't clear what's loaded), and add `provider.FetchBundles()` to the returned batch.
- `Update` top-level cases (before the overlay switch):
  - `provider.BundlesLoadedMsg` → when `msg.Err == nil`, `m.catalog = msg.Bundles`.
  - `provider.DefaultBundlesLoadedMsg` → if `msg.Err == nil && !m.bundlesEdited && slices.Equal(msg.WorkspaceIDs, m.rowIDs())` then `m.bundles = msg.Bundles; m.defaultsLoaded = true`. Stale or failed replies are ignored.
  - `setRows` resets `m.defaultsLoaded = false` whenever it requests defaults.
- Add `rowIDs() []uint` and use it in `openWorkspacePicker` (replacing the inline loop).
- `setRows`: after building rows, when `!m.bundlesEdited` append `provider.FetchDefaultBundles(m.rowIDs())` to the cmds (only when there is at least one row).
- `Keys()` / `CapturesText()` / `View()` / `Status()` handle `overlayBundles` like the other overlays.
- `onPageKey`:
  - The existing "Down from Workspaces resets `rowCursor`" special case now applies to `fieldBundles` (the field right above Branches): `if field == fieldBundles && msg.Type == tea.KeyDown { m.rowCursor = 0 }`.
  - `formKeys.Edit` on `fieldBundles` opens the picker: `m.bundlePicker = m.bundlePicker.Open(m.catalog, m.bundles); m.overlay = overlayBundles`.
- `updateBundlePicker`: `SelectedMsg` → `m.bundles = msg.Bundles`, `m.bundlesEdited = true`, close overlay, `m.focus(fieldBundles)`. `ClosedMsg` → close overlay, `m.focus(fieldBundles)`. Otherwise forward to the picker.
- `submit`: `var selected []string; if m.bundlesEdited { selected = m.selectedBundles }` (the picker guarantees non-nil on select) and pass to `provider.CreateSession(name, specs, selected)`.
- `fields()` Bundles value, in order:
  - edited or defaults loaded with names → `strings.Join(m.selectedBundles, " · ")`, with hint `from workspace defaults` while not edited;
  - not edited and `!m.defaultsLoaded` → muted `workspace defaults` (the daemon will apply them; we just don't know them yet);
  - otherwise empty → muted `none` (or muted `no bundles configured` when `len(m.catalog) == 0`).

**Step 1: Write failing tests** (reuse `started`, `picked`, `loaded`, `press`; `uitest.Drain` to inspect messages):

- `TestForm_PickingWorkspacesRequestsDefaultBundles`: `picked(t, api, web)` cmd contains `provider.fetchDefaultBundlesIntentMsg`.
- `TestForm_DefaultBundlesPreCheck`: after `picked(t, gitWS(1,"api"))`, `Update(provider.DefaultBundlesLoadedMsg{WorkspaceIDs: []uint{1}, Bundles: []string{"go"}})` → `m.bundles == []string{"go"}` and the view contains `go` and `from workspace defaults`.
- `TestForm_StaleDefaultBundlesIgnored`: rows are `[1]`, message for `[1,2]` → `m.bundles` stays nil.
- `TestForm_BundlesShowWorkspaceDefaultsUntilLoaded`: right after `picked`, the view's Bundles line contains `workspace defaults`; a failed `DefaultBundlesLoadedMsg{WorkspaceIDs: []uint{1}, Err: errors.New("x")}` leaves it that way and leaves `Status()` empty and `submitting` untouched.
- `TestForm_EditedBundlesStopRecompute`: load catalog (`BundlesLoadedMsg`), load defaults `["go"]`, focus Bundles (`shift+tab` until `m.form.Focused() == fieldBundles`, or set focus via `m.focus(fieldBundles)`), `enter` opens `overlayBundles`, `Update(bundlepicker.SelectedMsg{Bundles: []string{"docs"}})` → `bundlesEdited`, overlay closed, `bundles == ["docs"]`. Then a new `DefaultBundlesLoadedMsg{WorkspaceIDs: []uint{1}, Bundles: []string{"go"}}` leaves `["docs"]`; and changing workspaces via `workspacepicker.SelectedMsg` does **not** emit `fetchDefaultBundlesIntentMsg`.
- `TestForm_SubmitOmitsBundlesUntilEdited`: create a session without touching bundles; drain the submit cmd, assert the `createSessionIntentMsg` has `bundles == nil` (same package, so the unexported field is readable).
- `TestForm_SubmitSendsEditedEmptyBundles`: edit to `bundlepicker.SelectedMsg{Bundles: []string{}}`, submit → `bundles` non-nil and empty.
- Update `TestForm_OpensIntoWorkspacePickerAndReturnsToName` and any test relying on field order or `Init` cmd contents if they now fail (e.g. `Init` returns an extra `fetchBundlesIntentMsg`). Keep assertions intent-preserving.

**Step 2:** run → fail. **Step 3:** implement. **Step 4:** `GOFLAGS=-count=1 task test --force` → PASS.

**Step 5:** Commit `feat(tui): bundles field in new-session form`.

---

### Task 5: Session detail: Bundles section and `p` to edit

**Files:**
- Modify: `internal/tui/sessiondetail/model.go`, `keys.go`, `panel.go`
- Test: `internal/tui/sessiondetail/model_test.go`, `panel_test.go`

**Changes:**

- `keys.go`: add `Bundles: key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "plugins"))`; add it to the third `FullHelp` group (`{k.Archive, k.Delete, k.Repair, k.AddWorkspace, k.Bundles}`).
- `model.go`:
  - Fields `catalog []bundles.Entry`, `bundlePicker bundlepicker.Model`, `pickingBundles bool`. `New()` returns `Model{bundlePicker: bundlepicker.New("Plugin bundles")}`.
  - `SelectMsg`: also close the bundle picker (`pickingBundles = false`) and return `tea.Batch(provider.RequestBrowserState(), provider.FetchBundles())`.
  - `tea.WindowSizeMsg`: also `m.bundlePicker.SetSize(msg.Width, msg.Height)`.
  - `provider.BundlesLoadedMsg` → when `msg.Err == nil`, `m.catalog = msg.Bundles`.
  - `provider.SessionBundlesSetMsg` → if it's this session: `m.setStatus("bundles saved · run /reload-plugins in open Claude panes", ui.StatusInfo)`.
  - `bundlepicker.SelectedMsg` → `pickingBundles = false`, return `provider.SetSessionBundles(m.sess.ID, msg.Bundles)`.
  - `bundlepicker.ClosedMsg` → `pickingBundles = false`.
  - **While `pickingBundles`, every message `Update` doesn't explicitly handle (keys and anything else, e.g. `list.FilterMatchesMsg`) must be forwarded to `m.bundlePicker.Update`.** The bubbles list applies filter results via its own async message; dropping it in the default `return m, nil` breaks filtering. Do this as the final fallthrough in `Update` and at the top of the `tea.KeyMsg` path (before `onKeyMsg`'s own handling).
  - `onKeyMsg`, new case: `key.Matches(msg, keys.Bundles)`: ignore when `m.sess.Status == session.StatusDeleted`; otherwise `m.bundlePicker = m.bundlePicker.Open(m.catalog, m.sess.Bundles)`, `m.pickingBundles = true`.
  - `Keys()`: when `pickingBundles` return `m.bundlePicker.Keys()`.
  - Add `CapturesText() bool { return m.pickingBundles && m.bundlePicker.CapturesText() }` so the filter can type `?`.
  - `Status()`: while picking, return `ui.Status{}`.
- `panel.go`:
  - `View()`: when `m.pickingBundles`, return `ui.FitHeight(m.bundlePicker.View(), m.height)` (before building the panel).
  - After the Workspaces section, when `len(s.Bundles) > 0`: `sectionRule(sectionTitleStyle().Render("Bundles"), mutedStyle().Render(fmt.Sprint(len(s.Bundles))), bw)` then one line `bundleLine(s.Bundles, m.catalog, bw)`: names joined with muted ` · `; when `m.catalog != nil` and a name is not in it, render it muted with ` (unknown)`. Then a blank line. `RenderPanel` (session-list preview) has no catalog, so names render plainly.

**Step 1: Write failing tests:**

`model_test.go`:
- `TestSessionDetail_SelectFetchesBundles`: `SelectMsg` cmd contains `provider.fetchBundlesIntentMsg`.
- `TestSessionDetail_PluginsKeyOpensPickerAndSaves`: session with `Bundles: []string{"go"}`, feed `BundlesLoadedMsg` (go, docs), press `p` → view contains `[x] go`; `Update(bundlepicker.SelectedMsg{Bundles: []string{"docs"}})` → cmd drains to `provider.setSessionBundlesIntentMsg` (check fields), picker closed (view shows the panel again).
- `TestSessionDetail_PluginsKeyIgnoredWhenDeleted`.
- `TestSessionDetail_BundlePickerFilters`: press `p`, `/`, type `go`, feeding every drained message (including async filter messages) back into the model until none remain; the view shows the `go` row and not the `docs` row.
- `TestSessionDetail_EscClosesBundlePicker`: `p`, then esc → drain cmd and feed the resulting `ClosedMsg` back; picker closed; esc did **not** navigate back (no `router` back message in the first cmd).
- `TestSessionDetail_BundlesSavedShowsReloadHint`: `Update(provider.SessionBundlesSetMsg{SessionID: id})` → `Status().Text` contains `/reload-plugins`.

`panel_test.go`:
- `TestRenderPanel_ShowsBundles`: session with `Bundles: []string{"go","docs"}` → `RenderPanel` contains `Bundles` and `go · docs`.
- `TestRenderPanel_NoBundlesSection`: no bundles → no `Bundles` heading.
- `TestSessionDetail_UnknownBundleMarked`: catalog has only `go`; session bundles `["go","legacy"]` → view contains `legacy (unknown)`.

Look at existing helpers (`makeActiveSession`, `runeKey`) in the test files and reuse them.

**Step 2:** run → fail. **Step 3:** implement. **Step 4:** `GOFLAGS=-count=1 task test --force` → PASS.

**Step 5:** Commit `feat(tui): edit plugin bundles from session detail`.

---

### Task 6: Docs and final checks

**Files:**
- Modify: `CLAUDE.md` (sessions route list: add `GET /sessions/default-bundles`; module list: add `internal/tui/bundlepicker`)
- Modify: `README.md` ("Plugin bundles" section: the form field, `p` in session detail, the `/reload-plugins` note)
- Modify: `docs/plans/2026-10-16-plugin-bundles-design.md` TUI section: `b` → `p` and the "sent only when edited" behavior

**Steps:**
1. Edit the docs.
2. `task fmt`, `task lint` (only the pre-existing `internal/db/dbservice.go` finding is acceptable), `GOFLAGS=-count=1 task test --force`.
3. Commit `docs: plugin bundles TUI`.
