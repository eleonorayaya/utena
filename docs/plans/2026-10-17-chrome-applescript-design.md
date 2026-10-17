# Chrome without an extension: AppleScript driver, tab lifecycle, a window for every session

## Why

On the work machine, Chrome extensions can't be installed, so the webext driver never connects there and utena falls back to `/usr/bin/open`. AppleScript control of Chrome is allowed (chrome-cli works). This adds a driver that drives Chrome through `osascript`, and two behaviors it needs: links for background sessions wait as pending tabs, and every attached session gets a window, so there is always a window to add tabs to by hand.

## What AppleScript can do quietly

Measured against Chrome on macOS with AeroSpace:

| Action | Takes focus? |
|---|---|
| Create a window (empty or with URLs, AppleScript or `--new-window`) | yes, within ~0.5s |
| Add a tab with a URL, or set a tab's URL | yes, within ~0.3s, and switches to the window's workspace |
| Add an empty tab | no |
| Switch a window's active tab | no |
| Read windows and tabs | no |
| `execute javascript` | blocked unless "Allow JavaScript from Apple Events" is on |

Loading a URL brings Chrome to the front, and there is no way to load one quietly. Putting focus back afterwards through AeroSpace works but flickers.

## Driver: `internal/chromescript`

A `browser.Driver` selected with `--browser chrome-applescript` (or `{"browser": "chrome-applescript"}` in `daemon.json`).

- **Polling:** one `osascript` call every ~1.5s returns every window's ID, its tabs' IDs, URLs and titles, the active tab index, the window order, and whether Chrome is frontmost. Diffing consecutive polls produces the events the webext driver sends: `window_created`, `window_removed`, `tabs_changed`, `focus_changed`. The script checks whether Chrome is running first, so polling never launches it.
- **Connection:** Chrome running means `connected`, Chrome stopped means `disconnected`. Chrome's PID is the run ID, so a Chrome restart triggers reattach.
- **Actions:** `make new window`, `make new tab`, `set active tab index`, `close window id`, and bringing a window to the front (`set index`) before the wm focus.
- **Capabilities:** the driver reports that creating a window takes focus and that loading a URL takes focus. The webext drivers report neither.
- **Errors:** every call has a 3s timeout. Three failed polls in a row count as disconnected. Error `-1743` (Automation not allowed) shows a status hint: "allow utena to control Chrome in System Settings → Privacy & Security → Automation".

## Tab lifecycle

Windows keep their lifecycle (`open`, `missing`, `snapshotted`). Tabs get one of their own:

```
pending ──load──► open ──window leaves open──► saved
   ▲                                             │
   └──────────────── window restored ◄───────────┘ (saved and pending load as open)
```

- `TabState` is `pending`, `open` or `saved`.
- **Only `pending` is stored**, on the tab entry in the row's `Tabs` JSON. `open` and `saved` are derived from the window state when the row is read: tabs of an `open` window are `open`, tabs of a `missing` or `snapshotted` window are `saved`. The bulk state updates (`MarkMissingExcept`, `Reopen`) never rewrite the tab JSON, so storing `open`/`saved` would go stale. Old rows need no migration.
- **One merge rule for every write from a live window:** the saved tabs become the live window's web tabs plus every pending tab that no live tab matches by URL. Snapshots, archive, adopt, reattach and pending-close all go through it. Loading a pending tab, or restoring a window with pending tabs, therefore clears the pending entries once the tabs are live, without duplicates.
- **Reattach matching** ignores pending tabs, since they were never loaded in the browser.

## Pending tabs (drivers where loading a URL takes focus)

- `Open` for a session that isn't attached (and isn't being activated) adds `{url, state: pending}` to the session's most recently focused window row, or to its most recent restorable row, or to a new `snapshotted` row with no driver window. A URL the row already has is not added again. This works while the browser is disconnected. `OpenResult.Pending` is set, and `utena open` prints that the link is waiting.
- When an owned window gets focus, its pending tabs are opened as tabs, oldest first, and then the window is snapshotted.
- Restoring a window opens its saved and pending tabs together.
- Archive keeps pending tabs (they are saved and come back on restore). Delete drops them. A window closed by hand is forgotten with its pending tabs.

## A window for every attached session (all drivers)

When the attached session changes, or the browser connects, placement makes sure the attached session has a live window: it restores its saved windows as today, and if there are none, creates a window with no URLs (the browser's new tab page). It runs only on those transitions, not on every placement, so a window closed by hand stays closed until the session is attached again.

When creating a window takes focus (AppleScript), placement records the focused wm window first (new `wm.Driver.FocusedWindow`), waits up to ~1.5s for the new window to take focus, and focuses the recorded window again.

## TUI

`TabView` gets a `state`. Open tabs render normally and are selectable; saved tabs are muted; pending tabs are muted with a `◌` marker. Only open tabs with a live ID can be selected.

## PRs

1. utena: tab lifecycle, pending tabs, a window for every attached session, `wm.Driver.FocusedWindow`, capabilities, TUI.
2. utena: `internal/chromescript`, `--browser chrome-applescript`, Automation hint, docs.
3. dotfiles: the work profile writes `{"browser": "chrome-applescript"}`.
