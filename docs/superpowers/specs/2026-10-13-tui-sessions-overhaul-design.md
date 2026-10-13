# TUI Sessions Screen Overhaul — Design

## Goal

Rework the sessions screen (session list, detail panel, full-screen session detail) so it works equally well as a **quick switcher** and a **triage dashboard**. Make it feel like a standard TUI (gh-dash / k9s style columns) with Charm-style softness (borderless, whitespace, soft accents). Build the visual pieces as shared components so every other view can adopt them in later passes.

## Scope

In scope:
- `internal/tui/sessionlist` (list + split layout)
- `internal/tui/sessiondetail` (panel renderer + full-screen detail view)
- App-level footer/help chrome in `internal/tui/app.go` / `helpmodel.go`
- New shared package `internal/tui/ui`
- Semantic status tokens in `internal/tui/theme`

Out of scope (follow-up passes): todos, workspaces, session form, blocker form, progress, debug, status view. These keep working unchanged and pick up the shared footer automatically. Also out of scope: a `/` filter, per-PR CI glyphs, and daemon/API changes. All data the design needs is already in the `/sessions` payload.

## Layout

Split layout (list left, detail right) at widths ≥ 90 columns, as today. Below 90 columns the screen shows the list only. `i` / `→` opens the full-screen detail view, which uses the same renderer as the panel.

```
  Needs you 2                              │ checkout-flow-redesign           ◆ attached
  ! data-sync-backfill        ●1     12m   │ ✓ Ready for review · used just now
▌ ✓ checkout-flow-redesign  ◆ ●1 ◌1   now  │
  ✓ search-ranking-tweak      ●1     45m   │ api-service
                                           │   #4847  fix(ABC-123): stop scheduled jo…  ◌ draft
  Working 1                                │ web-app-api
  ~ ui-cleanup              ◆        now   │   #2236  ABC-123: update the onboarding …   ⊕ merged
                                           │   #4426  feat(ABC-123): add a settings p…   ⊕ merged
  Idle 2                                   │
  · email-digest-queue              53m    │
  · admin-dashboards          ◌1     2h    │
                                           │
  Blocked 2                                │
  · legacy-config-cleanup-v2         1h    │
  · web-app-api-next-steps-fol…      1h    │

  enter switch · o open PR · n new · b block · a archive · ? more
```

## Session list

### Sections

Sessions are grouped into sections in this order. Empty sections are omitted.

| Section   | Membership |
|-----------|------------|
| Needs you | Not blocked, not hidden; aggregate Claude status is `needs_attention` or `ready_for_review` |
| Working   | Not blocked, not hidden; aggregate Claude status is `working` |
| Idle      | Not blocked, not hidden; aggregate Claude status is `idle`, `done`, or there are no Claude sessions |
| Blocked   | `IsBlocked()` and not hidden |
| Hidden    | `IsHidden()`. Only shown when hidden sessions are toggled on with `.` |

Within each section sessions are sorted by `LastUsedAt` descending. Blocked membership wins over Claude status. The attached session stays in its state section; it is not pinned.

Section headers are rendered as `Title N` (title in `Primary` bold, count in `TextMuted`), with no horizontal rule. There is one blank line between sections. Headers take up rows, so scrolling and offset maths must count them. The cursor skips headers.

When rows move between sections because of a state change, the cursor follows the selected session ID (existing `rebuildFiltered` behaviour).

### Columns

| Column    | Width | Content |
|-----------|-------|---------|
| Gutter    | 2     | `▌ ` on the selected row, otherwise blank |
| Status    | 2     | Claude glyph (see Glyphs), blank if no Claude |
| Name      | flex  | Session display name, truncated with `…` |
| Attached  | 2     | `◆` in `Primary` when `IsAttached`, else blank |
| PRs       | 7     | `●N` ready (open, non-draft) in `StatusReady`; `◌N` draft in `TextMuted`. Each part is omitted when its count is 0. Merged/closed PRs are not counted |
| Last used | 4     | Short relative time, right-aligned |

The selected row gets a soft full-row background (`Selection`) across all columns. The name is bold in `TextEmphasis`. Broken sessions render the name in `Error`; creating sessions render it in `StatusActive` (unchanged behaviour).

The blocker kind no longer appears in the list. Blocked state is communicated by the section.

### Empty state

With no sessions, the list shows `No sessions — press n to create one` in `TextMuted`.

## Detail panel

Structure, top to bottom:

1. **Title line.** The session name in `TextEmphasis` bold, left-aligned. `◆ attached` right-aligned when attached. The lifecycle pill (`broken`, `creating`, `archived`, `pending`) is right-aligned **only** when status is not `active`. Width maths must keep the pill inside the panel (fixes the current clipping).
2. **Status line.** `<glyph> <Claude status in words> · used <time>`. Wording: `Needs your input`, `Ready for review`, `Working`, `Idle`, `Done`. When there's no Claude session, show only `used <time>`.
3. **Rule.** One `SurfaceVariant` rule, followed by a blank line.
4. **Problem callout** (only if present). `StatusError` and failed `SessionActions` errors, in warning style, prefixed `⚠`.
5. **Blockers** (only if open blockers exist). A `Blocked` section header, then one row per blocker: `kind · label · time`. In interactive (full-screen) mode the cursor, `r` resolve, and `x x` delete behave as today.
6. **Workspaces and PRs.** One sub-header per session worktree, in `Position` order, using the workspace name (`Tertiary` bold). Each PR is listed under the workspace whose worktree `RepoID` matches the PR's `RepoID`:
   ```
     #4847  fix(ABC-123): stop scheduled jobs from…   ◌ draft
   ```
   - `#N` is an OSC 8 hyperlink to `HTMLURL`.
   - The title is truncated to fit.
   - The PR state badge is right-aligned.
   - Order: open, then draft, then merged, then closed; number descending within each state.
   - A workspace with no PRs shows `no pull requests` in `TextMuted`.
   - PRs whose `RepoID` matches no worktree go under an `Other` sub-header.
   - A session with no worktrees shows its `WorkspaceDisplay()` as the only sub-header.

Removed from the panel: branches, past branches, session root, the Tmux section, and the Claude section.

### Empty state

With no session selected, the panel shows `Select a session` in `TextMuted`.

## Opening PRs

- **OSC 8.** PR numbers in the detail panel are wrapped in OSC 8 hyperlinks so they can be ⌘-clicked. Verified working through tuios on 2026-10-13.
- **`o` key** (list and full-screen detail):
  - With exactly one open or draft PR on the selected session, open it with `open <url>` (same mechanism as `prlist`).
  - With several, show an inline picker in the footer area. It lists the open and draft PRs (`j`/`k`, `enter` opens, `esc` cancels).
  - With none, the footer shows `no open pull requests`.

## Glyphs and colour

All status glyphs and colours are defined once, in `internal/tui/ui`, and backed by new semantic theme tokens.

| Claude status      | Glyph | Colour token (new → default) |
|--------------------|-------|------------------------------|
| `needs_attention`  | `!`   | `StatusAttention` → `Primary` |
| `ready_for_review` | `✓`   | `StatusReview` → `AccentMint` |
| `working`          | `~`   | `StatusWorking` → `AccentLavender` |
| `done`             | `✓`   | `StatusDone` → `TextMuted` |
| `idle`             | `·`   | `StatusIdle` → `TextMuted` |

| PR state | Badge      | Colour |
|----------|------------|--------|
| open     | `● open`   | `StatusReady` |
| draft    | `◌ draft`  | `TextMuted` |
| merged   | `⊕ merged` | `Tertiary` |
| closed   | `✕ closed` | `Error` |

Glyphs are coloured foreground text; the filled background blocks in `statusview.StatusBadge` are not used on this screen. (`StatusBadge` stays as-is for the status view until that view is migrated.) New theme tokens get JSON keys (`status_attention`, `status_review`, `status_working`, `status_done`, `status_idle`) and default to the existing colours listed above, so custom theme files keep working.

## Time format

New `ui.ShortAgo(t)`: `now` (< 1m), `Nm`, `Nh`, `Nd`, `Nw`. It's used in list rows and the detail status line. `common.TimeAgo` stays as-is for other views.

## Footer and help

- **App-level footer, one line.** It shows the active view's short-help hints as `key desc · key desc · …` in `TextMuted`, with keys in `Primary`. Hints that don't fit the width are dropped from the end, and `? more` is always kept (unless the active view is capturing text). Multi-line status text is flattened to one line, and the footer never exceeds the width.
- **`?` toggles full help.** It replaces the whole view with a `Keys` screen showing the bubbles full-help grid (grouped columns), capped to the window height. Press `?` or `esc` to close it. The debug view moves from `?` to `ctrl+g`.
- **Text entry keeps `?`.** Views can implement an optional `ui.TextCapturer { CapturesText() bool }`. The forms (session, todo, blocker, workspace clone, add workspace) always capture, and list views capture while filtering. While a view captures text, `?` is passed through and `? more` is hidden.
- **Status and confirmation messages.** The footer gets them from an optional view interface:
  ```go
  type StatusReporter interface { Status() ui.Status }
  ```
  `ui.Status` carries `Text` and `Kind` (`Info` / `Warning` / `Error`). When a status is non-empty, it **replaces** the hints for that render. Two-press confirmations ("press d again to delete …") use `Warning`. `sessionlist` drops its own `renderFooter` and implements `StatusReporter` instead.
- **Height accounting.** The app subtracts 2 rows (a blank spacer plus the footer line) before sending `WindowSizeMsg` to views. The session views render exactly their height. The other views keep their existing `padToHeight(m.height-1)`, which already renders exactly `m.height` lines. This fixes the footer disappearing in the previous build.
- **Session list short help:** `enter switch · o open PR · n new · b block · a archive · ? more`. Full help also lists `d delete`, `i info`, `t todos`, `w workspaces`, `. hidden`, `q quit`.
- Views that don't implement `StatusReporter` (every other view, for now) get the new footer with their existing keymaps and no other change.

## Shared package: `internal/tui/ui`

Each piece has a single purpose and is tested on its own:

| File | Exposes |
|------|---------|
| `text.go` | `Truncate(s string, width int) string`, `FitHeight(s string, h int) string`, `ShortAgo(t time.Time) string`, `Hyperlink(url, text string) string` (OSC 8; visible width = width of `text`) |
| `status.go` | `ClaudeGlyph(status) Span`, `ClaudeStatusText(status) string`, `PRBadge(state) string`, `PRCounts(prs) (ready, draft int)`, `PRCountSpans(ready, draft int) []Span`, `SectionHeader(title string, count int) string` |
| `row.go` | `Span`, `Cell`, `Align`, `Row(cells, width, bg)`: fixed and flex columns, truncation, right-alignment, full-row background for the selected row; output never exceeds `width` |
| `footer.go` | `Footer(bindings []key.Binding, status Status, width int, showMore bool) string`, `Status`, `StatusKind`, `StatusReporter`, `TextCapturer`, `MoreKey` |
| `prpicker.go` | `OpenablePRs(prs)`, `PRPicker` (`Update`, `Sync`, `Height(maxRows)`, `View(width, maxRows)`, windowed around the cursor when rows are short), `OpenURL(url) tea.Cmd`, `OpenURLFailedMsg` |

Width calculations must treat OSC 8 sequences as zero-width. Verified: `lipgloss.Width` (x/ansi) already does.

## Data flow

No API or daemon changes. Everything is derived client-side from `[]session.Session` in `SessionsStateUpdatedMsg`:
- section membership: `IsHidden`, `IsBlocked`, `claude.AggregateStatus`
- PR counts and grouping: `PullRequests`, `Worktrees[].Worktree.RepoID`, `Worktrees[].Workspace.Name`

Grouping and sorting live in pure functions in `sessionlist`, e.g. `groupSessions([]session.Session, showHidden bool) []sectionGroup`. Grouping PRs by workspace is a pure function in `sessiondetail`.

## Error handling

- `provider.ErrMsg` is shown as an `Error` status in the footer, and clears on the next key press (current behaviour, new presentation).
- A failed `open <url>` is shown as an `Error` status.
- Broken sessions keep the existing two-press `enter` repair flow, with the prompt shown in the footer as `Warning`.

## Testing

- Unit tests for `ui`: `ShortAgo` boundaries, `PRCounts`, `PRCountSpans`, `Hyperlink` visible width, `Truncate`, `Row` column widths, `Footer` truncation and status replacement.
- `sessionlist`:
  - section grouping and order for every Claude status, blocked, hidden, and attached
  - the cursor skips headers and follows the selected ID across a regroup
  - scroll offset with headers present
- `sessiondetail`:
  - PRs grouped by workspace, including empty workspaces and the `Other` group
  - PR ordering
  - the pill only appears for non-active status, and the title line never exceeds the width
- Existing `model_test.go` tests are updated where the output intentionally changed.
- Manual: run in tuios at ~200×40 and ~80×24. Confirm OSC 8 click-through, the footer is visible, and the narrow fallback works.

## Follow-ups (not in this pass)

Migrate todos, workspaces, forms, progress, debug, and the status view to `ui` components; `/` filter; per-PR CI glyph.
