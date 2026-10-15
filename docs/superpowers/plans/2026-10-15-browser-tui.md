# Browser TUI (PR 4) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** The TUI shows each session's browser windows:
- **Session list:** a marker on sessions that have browser windows.
- **Detail panel:** a "Browser" section with the tab count, the window state, and the tabs grouped by window.
- **Tab select:** selecting a tab activates it and focuses its window, switching to the session first if needed.
- **Footer:** a warning when the extension is outdated.

**Stacks on:** PR 3 (`eqt/utena-window-lifecycle`, utena#126). This branch is `eqt/utena-window-tui`.

**Spec:** `docs/superpowers/specs/2026-10-15-browser-window-management-design.md`, §UX "TUI" plus all the amendments. This is option B from design question 10.

**Rules:**
- Run `task` from the repo root only; never `go` directly, never `cd`, never install tasks.
- No code comments.
- Work TDD.
- One commit per task, ending with a blank line and then `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.
- The known flaky test is `internal/session` `TestSessionService_AddWorkspace_FullIntegration`.
- Before any TUI work, invoke the `utena:bubbletea-tui` skill.

---

### Task 1: Daemon browser state endpoints

**Files:** `internal/browser/{service,controller,router,driver}.go`, `internal/chrome/driver.go`, plus tests.

- **`GET /browser/sessions`** returns `{"connected": bool, "outdated": bool, "sessions": {"<id>": SessionBrowser}}`.
  - `SessionBrowser{State string, TabCount int, Windows []WindowView}`.
  - `WindowView{ID string, State string, Focused bool, Tabs []TabView}`.
  - `TabView{ID, Title, URL string, Active bool}`.
- **Building the response:**
  - Read the non-deleted rows from the store.
  - **Open rows:** take the tabs from a live `ListWindows`, when connected, with a short timeout of about 1 s.
  - **`missing` and `snapshotted` rows:** take the tabs from the row's saved snapshot; they have no tab IDs.
  - `SessionBrowser.State` is the "best" state among the session's rows, ranked `open` > `missing` > `snapshotted`.
  - Sessions with no rows are omitted.
- **Locking:** this must not take `s.mu`, which can be held for up to about 15 s. Use only store reads plus a driver call. The driver is safe for concurrent use.
- **`outdated`:**
  - Add an optional interface `StatusReporter { Outdated() bool }` in `browser`.
  - `chrome.Driver` sets an atomic flag when it rejects a hello because of a protocol mismatch, and clears it on a good hello.
  - `GET /browser/status` also returns `outdated`.
- **`POST /browser/focus`** takes `{session_id, window_id, tab_id}`.
  - Add a `Service.FocusTab(ctx, req)` that follows the same `Open` rules for activating and focusing:
    - if the session isn't attached, activate it (`Activated` is set in the result);
    - place the window before focusing it;
    - `ActivateTab`;
    - browser focus, then `wm` focus, never focusing a window that is still on the park workspace.
  - It uses `context.WithoutCancel` with a 15 s budget, like `/open`.
  - Reject with 400 if the window isn't owned by the session.
  - Return 503 when unavailable.
  - Reuse `Open`'s focus helpers; don't duplicate them.
- **Tests:**
  - the service/controller response shape for open, missing and snapshotted rows;
  - no `s.mu` is taken: the request completes while the test holds `s.mu`;
  - `outdated` set after a mismatch and cleared after a good hello;
  - `FocusTab` for an attached session and an unattached session (activates, places, focuses);
  - a window not owned by the session is rejected;
  - 503 when unavailable;
  - the origin check applies to the new routes.

Commit: `feat(browser): browser state and tab focus endpoints`

### Task 2: TUI provider

**Files:** `internal/tui/provider/{client.go,provider.go,browserprovider.go}` and tests.

- Add `browserProvider`, which holds the latest `BrowserState{Connected, Outdated bool; Sessions map[uint]SessionBrowser}`.
  - Reuse the `browser` package's view types; the TUI already imports `internal/browser`.
  - It fetches on start, whenever sessions are fetched (hook into `fetchSessionsIntentMsg` / `sessionsLoadedMsg`), and after a successful focus.
  - It emits `BrowserStateUpdatedMsg{State BrowserState}`.
  - If the fetch fails, it keeps the previous state and logs nothing noisy. A daemon running an old version may 404; treat that as an empty state.
- Add `provider.FocusTab(sessionID uint, windowID, tabID string) tea.Cmd`, which sends a `FocusTabIntentMsg`.
  - The client POSTs `/browser/focus` with `http.DefaultClient` and a 20 s timeout.
  - On success with `Activated`, it returns `SessionSwitchedMsg{}` so the TUI quits, the same as `o`.
  - On error, it returns an `ErrMsg`.
- Wire the provider into `rootProvider`.
- **Tests:** an httptest server for the fetch, a 404, focus success and activation, and a focus error.

Commit: `feat(tui): fetch browser state and focus tabs through the daemon`

### Task 3: Session list marker

**Files:** `internal/tui/sessionlist/{sessionlist.go,rows.go}` and tests.

- Store the `BrowserState` from `BrowserStateUpdatedMsg`.
- Add a 2-column browser cell next to the attached marker:
  - `◫` in `theme.Current.AccentBlue` when the session's state is `open`;
  - `◫` muted when it is `missing` or `snapshotted`;
  - blank when the session has no rows.
- Keep the row width math correct; the existing tests for row width and truncation must pass.
- **Footer:** when `Outdated` is set, show a warning status: "browser extension outdated — reload it in chrome://extensions". Use the existing status mechanism, and don't overwrite a more recent action status.
- **Tests:** the marker for each state, absence, and the outdated warning.

Commit: `feat(tui): mark sessions with browser windows`

### Task 4: Detail panel Browser section and tab select

**Files:** `internal/tui/sessiondetail/{model.go,panel.go,keys.go}` and tests.

- Add a **Browser** section after Workspaces, rendered with `sectionRule("Browser", "<n> tabs · <state>")`. There is one line per tab: an active marker, then the title, then the muted host.
  - Group the tabs under a muted "window N" sub-heading only when there is more than one window.
  - For `missing` and `snapshotted` windows, render the tabs muted.
  - Omit the section when the session has no rows.
- **Selection:**
  - Extend the interactive cursor so it covers blockers and then the tabs of open windows, as one ordered list of selectable items. Up/down moves across both.
  - Blocker actions (`u`, `x`) apply only when the cursor is on a blocker.
  - `enter` on a tab calls `provider.FocusTab(sess.ID, windowID, tabID)`.
  - `enter` elsewhere keeps the current switch behaviour.
  - Update the key help so `enter` reads "open tab" when the cursor is on a tab.
  - Clamp the cursor when the browser state or the blockers change.
- **Tests:**
  - rendering with one window, with several windows, and with snapshotted windows;
  - the section is omitted when there are no rows;
  - the cursor moves from blockers into tabs;
  - `enter` on a tab emits `FocusTabIntentMsg` with the right IDs;
  - `enter` on a blocker or with no selection keeps the switch behaviour;
  - clamping after the state changes.

Commit: `feat(tui): browser section with tab select in session detail`

### Task 5: Docs

- **README** "Browser windows": describe the TUI marker, the detail section, the tab select and the outdated warning.
- **CLAUDE.md:** mention the `/browser/sessions` and `/browser/focus` routes.
- **Spec:** add a "PR 4 amendments" section.

Commit: `docs: browser windows in the TUI`

### Task 6: Smoke test (controller)

1. `task install`.
2. Open links for two sessions.
3. Check in the popup TUI that the marker shows for both sessions.
4. Check that the detail panel shows their tabs.
5. Select a tab of a non-attached session: it switches to that session, places its window and focuses that tab.
