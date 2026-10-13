# TUI Overhaul, Part 2 — Workspaces, Forms, Pickers, Session Progress

## Goal

Bring the rest of the TUI onto the visual system from part 1 (`2026-10-13-tui-sessions-overhaul-design.md`): sectioned column lists, soft selection, the app footer and status line, and `internal/tui/ui` components. Make the new-session flow fast for the common case of 2–4 workspaces on their default branches.

Depends on `2026-10-13-ui-removals-design.md` shipping first. Todos, the status view, the debug view, workspace detail and the workspace PR list are already gone.

## Scope

| View | Package |
|------|---------|
| Workspaces | `internal/tui/workspacelist` |
| New session | `internal/tui/sessionform` |
| Add workspace to session | `internal/tui/addworkspaceform` |
| Add blocker | `internal/tui/blockerform` |
| Clone workspace | `internal/tui/workspacecloneform` |
| Pickers | `internal/tui/workspacepicker`, `internal/tui/branchpicker`, `internal/tui/filepicker`, `internal/tui/list` |
| Session progress | `internal/tui/sessionprogress` |

Out of scope: the session list and detail (done in part 1), and the daemon, except for the one API field in *New session → Default branches*.

## Shared components (new, in `internal/tui/ui`)

| File | Exposes |
|------|---------|
| `listdelegate.go` | `ListDelegate`: a `bubbles/list` `ItemDelegate` rendering each item as one `ui.Row` with the `▌` gutter and `Selection` background on the selected row. Items implement `ui.RowItem { Cells(selected bool) []Cell }`. Height 1, spacing 0. |
| `listframe.go` | `StyleList(*list.Model)`: hides the bubbles title bar, status bar, help and pagination dots. Enables the filter. The filter input renders as `/ query` in `TextMuted`, above the rows. Replaces `internal/tui/list.New` / `NewWithDelegate`. |
| `form.go` | `Form`: a title (`Primary` bold) plus an ordered list of `Field`s. `Field` has `Label`, a renderable `Value` (text input, choice or summary), an optional `Hint` and an optional `Error`. Rendering: `▌` gutter on the focused field, a fixed label column (width = longest label + 2, `TextMuted`), the value, then the error under its field in `Error`. `Form.Focus(i)`, `Next()`, `Prev()`. |
| `choice.go` | `Choice`: an inline single-select (`● new branch   ○ use existing`). `←/→` or `space` changes it when focused. |
| `steplist.go` | `StepList`: renders steps as `glyph label` with `✓` done (`StatusReady`), a spinner for running (`Primary`), `✕` failed (`Error`) with the error indented on the next line, and `·` pending (`TextMuted`). |

Every view below uses the app footer for hints and implements `ui.StatusReporter` for confirmations and errors. Text-entry views implement `ui.TextCapturer`.

## Workspaces

One screen, with no drill-in.

```
  Workspaces 5
▌ utena            eleonorayaya/utena    bare        2h
  api-service      acme/api-service      bare        1d
  web-app          acme/web-app          ◌ cloning…
  dotfiles         eleonorayaya/dotfiles not bare    3w
  scratch          —                     not git     2mo

  Hidden 1
  old-prototype    acme/old-prototype    bare        4mo

  ~/code/utena
```

- Sections: `Workspaces`, then `Hidden` (only when `.` toggles hidden on). Each section is sorted by `LastUsedAt` descending.
- Columns:
  - gutter (2)
  - name (flex)
  - repo full name, or `—` (flex, max 28)
  - kind: `bare`, `not bare`, `not git`, all `TextMuted`
  - status: `◌ cloning…` (`StatusActive`) and `✕ failed` (`Error`) replace the kind column
  - last used, as `ui.ShortAgo` (4, right-aligned)
- The last body line shows the selected workspace's abbreviated path in `TextMuted`.
- Keys:
  - `c` clone (opens the clone form)
  - `m` migrate to bare (only for git, non-bare workspaces; otherwise the status says `already bare` or `not a git repository`). This is a two-press confirmation: `press m again to migrate <name> to bare`.
  - `h` hide / unhide
  - `d d` delete (two-press: `press d again to delete <name>`)
  - `.` show hidden
  - `/` filter
  - `esc` back
- Short help: `c clone · m migrate · h hide · d delete · ? more`
- Empty state: `No workspaces — press c to clone one`.

## New session

A single page. Every field is visible, and pickers open in place of the page and return to it.

```
  New session

  Workspaces   api-service, web-app, data-sync
▌ Branches     api-service  main
               web-app      main
               data-sync    main
  Mode         ● new branch   ○ use existing
  Name         checkout-flow-redesign

```

- **Fields, in order:** Workspaces, Branches, Mode, Name. `tab`/`↓` go to the next field and `shift+tab`/`↑` to the previous one. In Branches, `↑/↓` move between the workspace rows first and leave the field at its edges.
- **Initial focus:** the page opens straight into the workspace picker, which is the step you always need. Confirming it returns to the page with focus on Name.
- **Workspaces:** `enter` opens the multi-select workspace picker. `a` inside the picker adds a directory (file picker), as today. Changing the selection keeps the branches already picked for workspaces that are still selected.
- **Branches:** each selected workspace gets one row, defaulting to that workspace's default branch (see below). `enter` on a row opens the branch picker for that workspace. Inside the picker, `n` types a branch name; the existence check and errors show inline under the row. A branch that isn't the default is shown in `TextEmphasis`; default branches are shown in `TextMuted`.
- **Mode:** a `ui.Choice`: `new branch` (default) or `use existing`.
- **Name:** a text input. Its validation error shows under the field.
- **Submit:** `enter` on Name creates the session. `enter` on the other fields opens or changes them. If any branch row is still loading, the status says `waiting for branch info` and nothing is submitted.
- **`esc`:** closes an open picker and returns to the page. On the page itself, it goes back to the session list.
- **Short help:** `tab next · enter edit · esc cancel` on most fields; on Name, `enter create · tab next · esc cancel`.

### Default branches

- When workspaces are confirmed, the form fetches `GET /workspaces/{id}/branches` for each new workspace, in parallel. A row shows `…` until its branches load.
- **API change:** the branches response gains `default_branch`, filled from `GitService.DefaultBranch` (origin/HEAD). The TUI's default is `default_branch`, then `current_branch`, then the first local branch, then `main`.
- A fetch error shows `✕ <error>` under that row, and the row stays editable.

## Add workspace to session

The same `ui.Form` with three fields: Workspace, Branch and Mode. Workspace and Branch use the same in-place pickers as the new-session form, and Branch defaults to the workspace's default branch the same way. Mode is the same `ui.Choice` as in the new-session form; it replaces today's `branchModeStep`. The form opens straight into the workspace picker. `enter` on Mode submits.

## Add blocker

A `ui.Form` with three fields:
- **Kind:** a `ui.Choice` over `session.BlockerKinds`.
- **Note:** text, with the placeholder `what are you waiting on?`.
- **URL:** text, with the placeholder `PR or Slack link`.

`tab`/`↑↓` move between fields, `enter` submits from any field, and `esc` cancels. Errors show under the field they belong to; otherwise they go in the footer status.

## Clone workspace

A `ui.Form` with two fields:
- **URL:** required, with the placeholder `git@github.com:org/repo.git`.
- **Directory:** optional, with the placeholder `derived from URL`.

`enter` submits from either field. While the clone request is in flight, the footer status shows `cloning…` and repeated submits are ignored. A validation or API error shows under URL.

## Pickers

- The workspace, branch and file pickers use `ui.ListDelegate` and `ui.StyleList`, with one-line rows.
- **Workspace picker:** gutter, then `[x]` / `[ ]` when in multi-select mode, then name, kind and last used. `space` toggles and `enter` confirms. `H` shows hidden and `a` adds a directory.
- **Branch picker:** two sections, `Local` then `Remote`, each sorted by name. The default branch row is suffixed `default` in `TextMuted`. `r` refreshes and `n` types a name.
- **File picker:** gutter, then a `▸` dir marker, then the name. `s` selects the directory and `.` shows hidden.
- Every picker shows its title as a `ui.SectionHeader`-styled line above the rows, e.g. `Branch for web-app`.

## Session progress

```
  Creating checkout-flow-redesign

  ✓ Fetch api-service
  ✓ Create worktree api-service
  ◌ Create worktree web-app
  · Start tuios session

```

- The title is `Creating <name>`, or `Repairing <name>` when opened from repair.
- Steps are rendered with `ui.StepList` from `SetupSteps`, in `Position` order. Before any steps exist, the view shows a single running step labelled `Setting up`.
- A **warning** (the session is active, but a step failed) shows as `⚠ <warning>` in the footer status as `Warning`. A **fatal error** shows `✕ <error>` as `Error`.
- Keys: `esc` returns to the session list (or opens the session once it is active, as today), and `r` retries when failed or warned. Short help shows only the keys that apply in the current state.

## Theme

No new tokens. The `List*` tokens (`ListTitle`, `ListDesc`, `ListSelectedTitle`, `ListSelectedDesc`, `ListSelectedBorder`) become unused, but stay in `Theme` and `themeFile` so existing theme files still load.

## Testing

- **`ui`:**
  - `ListDelegate` renders one line of exactly the list width, with the selection background only on the selected row.
  - `Form` focus movement, the label column width, and an error rendered under its field.
  - `Choice` cycling.
  - `StepList` glyph per status, with the failed step's error on the next line.
- **Workspaces:**
  - Sections and sorting, the kind/status column, the path line.
  - Two-press `m` and `d`, plus the `m` guards for bare and non-git workspaces.
  - Filter typing doesn't trigger actions (`CapturesText` while filtering).
- **New session:**
  - Opens into the workspace picker, and returns focus to Name.
  - Default branch resolution order.
  - Changing workspaces keeps existing picks.
  - Submit is blocked while branches are loading.
  - `enter` on Name submits.
  - `esc` from a picker returns to the page.
  - Every line fits the width with 4 workspaces and long names.
- **Blocker / clone / add-workspace forms:** field navigation, submit from any field, error placement, and ignoring repeated submits while cloning.
- **Pickers:** one-line rows, the branch picker's `Local`/`Remote` sections and `default` marker.
- **Session progress:** creating vs repairing title, step glyphs, a warning or error goes to the footer status, and the `r` hint only shows when relevant.
- **Daemon:** the branches endpoint returns `default_branch`.
- **Manual:** run the full new-session flow with 3 workspaces in the tuios popup at about 200×40 and 80×24.

## Follow-ups

None planned. A later pass could add a `/` filter to the session list.
