# herdr-utena: design

Status: proposal. Target: `plugins/herdr-utena/`, inside this module.

## Context

utena is a daemon + TUI + tmux plugin that manages multi-repo AI coding sessions.
[herdr](https://herdr.dev) is a terminal workspace manager for coding agents that natively
does most of utena's *substrate*: pane/session management, persistence across detach,
agent state detection (`blocked`/`working`/`done`/`idle`) across 16 agents, and single-repo
worktree handling. Everything utena built to support those is now duplicated effort —
the tmux plugin, the Claude status hook, the gotmux layer, and most of the daemon's reason
to exist.

What herdr does *not* have, and what utena's workflow actually depends on:

1. **Multi-repo sessions.** A herdr workspace is one repo; a worktree is a branch of that
   same repo. There is no construct grouping N independent repos as one unit of work.
2. **Generated multi-checkout `CLAUDE.md` + merged sandbox settings.** Nothing in the
   ~518 repos tagged `herdr-plugin` does this.
3. **PR-driven session lifecycle.** Auto-create a review session on PR assignment,
   auto-launch Claude with a review command, auto-complete on merge.
4. **PR/CI activity pushed into the agent's context** (utena's WebSocket monitor).

This plugin keeps 1–4 and deletes the rest. Outcome: utena's daemon, HTTP API, SQLite,
gotmux layer, tmux plugin, and Claude status hook all go away; the TUI and the
git/claude-settings logic survive.

## Scope

**In (v1):** multi-repo session create/list/archive/delete; generated `CLAUDE.md` +
merged `.claude/settings.local.json`; PR-driven session automation; PR/CI events pushed
into the agent.

**Out:** todos (use `herdr-board` / `herdr-beads`); tmux anything; agent status tracking
(native); the workspace clone/bare-migration flows (defer — herdr has no equivalent and
it's orthogonal to sessions); the daemon, HTTP API, and SQLite.

**Delegated:** the declarative setup pipeline, to `tdi/herdr-worktree-setup`. An audit found
no repo actually uses utena's version — no `.utena/config.json` in any registered workspace
and no global `~/.config/utena/worktree-setup` hook — so porting `workspaceconfig.go`,
`setupstepdefinition.go` and the `setupexecutor.go` DAG would be work for a feature with no
users. The installed plugin fires on the same `worktree.opened` event our checkouts emit.
`herdr-routines` is the other external dependency (below).

## Architecture

One Go binary, `plugins/herdr-utena/`, dispatching on `os.Args[1]` — the same
action→pane→binary shape `cloudmanic/herdr-plus` uses in production. It talks to a running
herdr server by shelling out to `$HERDR_BIN_PATH` and parsing the JSON each command returns.
No daemon, no HTTP server, no GORM/SQLite (which also drops the CGO build requirement).

```toml
# herdr-plugin.toml
id = "eleonorayaya.herdr-utena"
min_herdr_version = "0.8.0"

[[build]]
command = ["sh", "scripts/build.sh"]

[[actions]]
id = "sessions"                                    # opens the pane below
command = ["./bin/herdr-utena", "sessions"]

[[panes]]
id = "sessions-ui"
placement = "zoomed"
command = ["./bin/herdr-utena", "sessions-ui"]     # the bubbletea TUI

[[actions]]
id = "poll"                                        # invoked by herdr-routines, headless
command = ["./bin/herdr-utena", "poll"]
```

**Module placement is load-bearing.** Go's `internal/` rule is directory-tree scoped but
enforced against the *importing module's* root. `plugins/herdr-utena/` as part of the root
module can import `github.com/eleonorayaya/utena/internal/...` directly. A nested `go.mod`
**cannot**, even with a `replace`. So: no nested `go.mod`. This is also why we get
`internal/claudesettings` for free.

Caveat that shapes the reuse table: importing a *package* compiles the whole package.
`internal/session` is one 30-file package that pulls GORM, SQLite/CGO, chi, and gotmux —
so pure helpers living inside it must be copied, not imported.

### Background work

herdr plugins are event-driven with no daemon. `mrcndz/herdr-routines` supplies cron;
its `type = "plugin_action"` fires our action headlessly with no tab:

```toml
[[routine]]
name = "herdr-utena-poll"
every = "1m"
type = "plugin_action"
action = "eleonorayaya.herdr-utena.poll"
```

One tick does: sync PRs → diff against state → lifecycle actions → sync activity → emit
prompts. ~1 + 3N GitHub calls for N watched PRs. utena's `git.prs` job runs at 30s; 60s is
fine. A side benefit over the daemon: each tick is a fresh process, so `gh auth token` is
re-resolved instead of being cached for the daemon's lifetime.

## Session model

herdr has no session concept, and we do not try to add one to its data model. A session is
a **directory convention plus a state file**; herdr sees ordinary workspaces.

```
~/herdr-sessions/<name>/          # session root — the +1 workspace, agent runs here
  CLAUDE.md                       # generated, marker-guarded
  .claude/settings.local.json     # merged allowWrite for every checkout
  <repo-a>/                       # git worktree
  <repo-b>/                       # git worktree
```

Creation, per repo: `git worktree add` **directly** (not `herdr worktree create`, which
couples checkout creation to workspace creation and gives no control over placement), then
register it:

```
herdr worktree open --workspace <parent-repo-ws-id> --path <session-root>/<repo>
```

Verified against herdr 0.8.0: this accepts a worktree created by plain `git worktree add`,
returns full git provenance (`branch`, `repo_key`, `repo_root`, `is_linked_worktree`), fires
the same `worktree.opened` event other plugins hook, and is idempotent — a repeat call
returns `already_open: true` and reuses the existing workspace. So `herdr-worktree-setup`,
`herdr-plugin-gh-pr`, and herdr-plus auto-layouts all keep working against our checkouts.

**Do not use `herdr worktree open`.** It was the obvious choice and it is the wrong one.
Each call creates *two* workspaces — the checkout plus a parent-repo workspace — giving
2N+1 per session. The parent is structural: closing it **cascades and destroys its worktree
children**, so it cannot be cleaned away afterwards, and herdr has no way to hide a
workspace (no `hidden`/`visible` field in the API schema, no sidebar filter option).

Register each checkout as a plain workspace instead:

```
herdr workspace create --cwd <session-root>/<repo> --label <repo>
```

Plus one for the agent: `herdr workspace create --cwd <session-root>`. That is **N+1**,
with no parent rows at all.

The reason this is safe: **the sidebar's `branch` token reads from the workspace cwd, not
from the `worktree` binding.** Verified on 0.8.0 by registering an identical git worktree as
a plain workspace and confirming its distinct branch (`zz-unbound-probe`) rendered in the
sidebar exactly like bound rows. So the binding buys no per-repo git display that we lose.

What the binding *does* buy, and what N+1 forfeits: `worktree.created`/`worktree.opened`
never fire, so `tdi/herdr-worktree-setup` will not run against our checkouts, and
`herdr worktree list/remove` will not manage them (they remain ordinary git worktrees,
removed with `git worktree remove`). Given no repo currently uses setup actions, this costs
nothing today — but it is the tradeoff to revisit if that changes.

### Grouping without nesting

herdr cannot nest workspaces on demand. Confirmed against the full API schema: no parent,
child, group, indent or depth field exists anywhere, and nesting is derived solely from the
`worktree` binding's `repo_root`. Two socket-only calls provide the alternative — neither is
exposed by the `herdr` CLI, so the plugin dials `$HERDR_SOCKET_PATH` directly:

- `workspace.report_metadata` — `{workspace_id, source, tokens}`; tag every workspace in a
  session with `session=<name>`. Token names must match `^[A-Za-z0-9_-]{1,32}$`, max 16.
- `workspace.move_block` — `{workspace_ids}`; make the session's rows contiguous in one call,
  session root first.

Render it with a `$session` column, which lets checkout labels stay short (`api`, not
`<session>/api`):

```toml
[ui.sidebar.spaces]
rows = [["state_icon", "$session", "workspace"], ["branch", "git_status"]]
```

**herdr IDs are strings** (`w1`, `w3`, `w1:p1`) — verified live. utena's TUI threads `uint`
everywhere (`ActivateSession(id uint)`, `workspacepicker.selected map[uint]struct{}`,
`sessionprogress.Start(uint)`). This is a mechanical but wide change across pickers, msg
types, and provider signatures. Budget for it explicitly; it is the single most pervasive
edit in the port.

## What we reuse

| Component | Verdict | Notes |
|---|---|---|
| `internal/claudesettings/` | **import as-is** | stdlib only, no DB/HTTP/globals. `EnsureMultiSessionGuide`, `EnsureSessionRoot`, `LinkWorktree`. Change the `<!-- utena:multi-session-guide -->` marker → note that existing generated files then read as user-authored and are never rewritten. |
| `internal/tui/{theme,list,router,filepicker}` | **import as-is** | zero domain deps. |
| `internal/tui/branchpicker` | **import, 1-line tweak** | all-string API already; only consumes `provider.Branches*Msg`. |
| `internal/tui/workspacepicker` | **copy, new data layer** | multi-select UI already exists — `space` toggles, `enter` implicit-selects the row under the cursor. `map[uint]` → string. |
| `internal/tui/sessionform` | **copy, new data layer** | the 6-step machine (`workspacePicker → filePicker → branchPicker → branchManualInput → branchMode → nameInput`) is pure logic; ~8 `provider.X()` call sites change. Its tests drive it purely through messages and port with it. |
| `internal/tui/sessionprogress` | **copy, simplify** | polls at 200ms today; in-process now, so back it with a channel instead. |
| `internal/tui/sessionlist` | **copy, trim** | widest surface — drags in `sessiondetail`, `statusview`, `claude`, `git`. Port the list half; rewrite the detail panel. |
| `internal/tui/provider/*provider.go` | **keep** | caching, refetch-chaining, busy-polling are transport-agnostic. |
| `internal/tui/provider/client.go` | **rewrite** | 700 lines; **the entire data-layer change**. Every method already returns `tea.Cmd`, and nothing outside `provider/` calls it — so a `herdrClient` behind a small interface leaves all 18 view packages untouched. |
| `internal/git/gitcli.go` | **copy (~477 lines)** | zero deps, but `gitCLI` and its methods are unexported and `NewGitService` demands a `db.Database`. Exportize on copy. Drop `migrateToBare`. |
| `internal/git/githubclient.go` | **reuse verbatim** | `NewGitHubClient(ctx)` takes only a context. `$GITHUB_TOKEN` → `gh auth token`. |
| `internal/session/workspaceconfig.go` | **copy** | stdlib only; `.utena/config.json` schema unchanged. |
| `setupstepdefinition.go` + `setupexecutor.go` | **copy, rewire** | `buildSetupSteps` is pure. The executor's DB coupling funnels through one type, `stepUpdater`, at 3 call sites — swap for a progress callback (~15 lines). It already no-ops on unpersisted steps. Rewrite `execSetupWorktree`; delete `StepKindSetupTmux`. |
| `SanitizeSessionName` / `ValidateSessionName` | **copy (~25 lines)** | pure, but live in the GORM-laden `session` package. |
| daemon, HTTP API, SQLite, gotmux, tmux plugin, `utena-claude` hook, monitor WebSocket | **delete** | herdr covers all of it. |

Preserve the hook-script contract verbatim (`<ws>/.utena/worktree-setup`, global
`<configDir>/worktree-setup`, env `UTENA_SESSION_{NAME,ROOT}`, `UTENA_BRANCH`,
`UTENA_WORKTREE_PATH`, `UTENA_WORKSPACE_{NAME,PATH}`, missing file = no-op, failures
non-fatal) so existing repo configs keep working.

## State

Flat JSON under the plugin's `$HERDR_PLUGIN_STATE_DIR`. Two files, because they have two
different writers:

**`sessions.json`** — one record per session: name, root, status, `last_used_at`, and per
checkout `{repo, workspace_path, worktree_path, branch, herdr_workspace_id}`. Statuses
collapse to `creating | active | broken | completed | archived` (drop `pending`, which is
dead on utena's PR path; drop `inactive`/`deleted`, which were tmux/soft-delete artifacts).

**`prs.json`** — keyed `(repo, number)`: `state`, `title`, `is_assigned_to_me`, `html_url`,
`author_login`, `head_sha`, plus the watermarks `activity_baselined`, `last_review_id`,
`last_review_comment_id`, `checks_head_sha`, `checks_state`.

**These two writers must stay separate.** utena enforces this with
`prStore.Update(pr, activityColumns()...)` → GORM `Omit`, precisely because the PR sync
rebuilds a `PullRequest` from the GitHub payload and would otherwise blank the watermarks.
A naive "marshal the whole struct" write here replays all history every tick, forever.

## PR automation

Trigger conditions, from `sessionservice.go:handlePRUpdated`:

- `newlyAssigned` = `IsAssignedToMe && state == open && (isNew || !previous.IsAssignedToMe)`.
  `IsAssignedToMe` means GitHub *assignee* **or** *requested reviewer*; `currentUser` comes
  from `GET /user`. Note `state == open` is strict — drafts do not trigger.
- `newlyMerged` = `state == merged && (isNew || previous.state != merged)`.

On `newlyAssigned` with no existing session: create the session, then launch the review.
utena spawned `claude --permission-mode auto "<prompt>"` in a tmux window; the herdr
equivalent is `herdr tab create` in the session workspace, start the agent, then:

```
herdr agent prompt <target> "/git-workflows:better-review <pr-url>" 
```

On `newlyMerged`: status → `completed`. A later tick archives it when
`!attached && last_used_at < now-5m` (utena's effective delay is 5–10min, since a 5-minute
tick tests a 5-minute cutoff).

### Two cold-start guards are mandatory

utena needs only one because its SQLite DB is never empty. A flat file starts empty, twice over:

1. **Per-PR activity** — `activity_baselined`, as utena already does: on first sight of a PR,
   advance the watermarks but emit nothing.
2. **Per-PR existence** — utena's `previous == nil ⇒ isNew ⇒ newlyAssigned` path. On a first
   run, or a deleted/corrupt state file, **every open assigned PR looks newly assigned** and
   spawns a session and a Claude window each. Baseline the PR set on first sight without acting.

Also decide `on_create` re-fire semantics deliberately: utena re-dispatches on tmux revival
(`ActivateSession`), so the review prompt fires again every time the session is revived.
That is incidental, not designed. Default to one-shot; record that the action ran.

## PR/CI events into the agent

Same four payloads utena defines in `practivity.go` (`pull_request`,
`pull_request_review`, `pull_request_review_comment`, `ci_checks`), formatted as text and
delivered with `herdr agent prompt <target> <text>` instead of NDJSON over a WebSocket.
`cmd/tui/monitor.go` and `internal/monitor/` both delete.

Keep from the original: self-authored filtering (`login == currentUser`) but **not** bot
filtering — review bots do most of the reviewing; 600-rune body truncation; and the check
rollup's suppression rules (`checks_head_sha == ""` suppresses the first rollup; a new head
SHA invalidates the previous rollup).

Watermarks are keyed **per PR**; delivery is **per session**. Several sessions can sit on one
branch, and detecting activity consumes the watermark — so fan out to all watchers of a
branch from one detection pass, or the second session silently loses its notifications.

## Phases

1. **Skeleton** — manifest, build script, `main.go` dispatch, `herdrClient` shelling to the
   CLI. Prove the loop with a `ping` action.
2. **Session core** — state file, `git worktree add` + `worktree open` + `workspace create`,
   `claudesettings` wiring, setup pipeline. Headless `new-session` subcommand first, no TUI.
3. **TUI** — port the pickers and `sessionform` onto the new client; the `uint` → string ID
   sweep lands here.
4. **PR automation** — `poll` action, both cold-start guards, `herdr-routines` config.
5. **Event injection** — activity sync + `agent prompt` formatting.
6. **Delete** — daemon, HTTP API, SQLite, gotmux, tmux plugin, `utena-claude`, monitor.

## Verification

- **Session creation:** run `new-session` against two scratch repos; assert N+1 workspaces
  in `herdr api snapshot`, a marker-guarded `CLAUDE.md` at the root listing both checkouts,
  and `allowWrite` entries for both `.git` dirs in the root's `settings.local.json`.
  Re-run to confirm idempotency (`already_open: true`, no duplicate workspaces, user-edited
  `CLAUDE.md` left alone).
- **Setup pipeline:** a scratch repo with `.utena/config.json` (`copy_file` + `install_deps`)
  and an executable `.utena/worktree-setup`; assert the file landed, deps installed, and the
  hook saw all six env vars. Non-executable hook → error; missing hook → silent no-op.
- **Cold start (the risky one):** point the plugin at a real repo with ≥1 open assigned PR
  and **no state file**. Assert zero sessions created and zero prompts sent on the first
  tick, and that both baselines are recorded. Then assign a PR and assert exactly one
  session and one review prompt on the next tick.
- **Watermarks:** two ticks with no GitHub change → zero prompts. Confirms the two-writer
  split didn't blank anything.
- **Fan-out:** two sessions on one branch, one new review → both get a prompt.
- Port `sessionform`'s existing message-driven tests as-is; they need no server.
