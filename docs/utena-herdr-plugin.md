# utena as a herdr plugin: design

Status: phases 1–3 built and in use. Lives at `plugins/utena/`, inside this module.
Sections marked **verified** were measured against herdr 0.8.0, not inferred.

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
users. Note the plugin will **not** fire for our checkouts, since we no longer create
worktree bindings — see the session model. `herdr-routines` is the other external
dependency (below).

## Architecture

One Go binary, `plugins/utena/`, dispatching on `os.Args[1]` — the same
action→pane→binary shape `cloudmanic/herdr-plus` uses in production. It talks to a running
herdr server by shelling out to `$HERDR_BIN_PATH` and parsing the JSON each command returns.
No daemon, no HTTP server, no GORM/SQLite (which also drops the CGO build requirement).

```toml
# herdr-plugin.toml
id = "eleonorayaya.utena"
min_herdr_version = "0.8.0"

[[build]]
command = ["sh", "scripts/build.sh"]

[[actions]]
id = "sessions"                       # prefix+u — toggles the sidebar pane
command = ["./bin/utena", "open-sidebar"]

[[panes]]
id = "sidebar"
placement = "split"                   # opened leftmost, then swapped into place
command = ["./bin/utena", "sidebar"]

[[actions]]
id = "pick"                           # prefix+p — session-aware picker
command = ["./bin/utena", "open-pick"]

[[panes]]
id = "picker"
placement = "popup"                   # valid over the socket, not on the CLI flag
command = ["./bin/utena", "pick"]

[[actions]]
id = "poll"                           # invoked by herdr-routines, headless
command = ["./bin/utena", "poll"]
```

The binary talks to herdr two ways: the `herdr` CLI for anything it exposes, and the unix
socket at `$HERDR_SOCKET_PATH` for what it does not — `workspace.report_metadata`,
`workspace.move_block`, `events.subscribe`, `popup.close`, and `plugin.pane.open` with
`placement = "popup"`.

**Module placement is load-bearing.** Go's `internal/` rule is directory-tree scoped but
enforced against the *importing module's* root. `plugins/utena/` as part of the root
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
name = "utena-poll"
every = "1m"
type = "plugin_action"
action = "eleonorayaya.utena.poll"
```

One tick does: sync PRs → diff against state → lifecycle actions → sync activity → emit
prompts. ~1 + 3N GitHub calls for N watched PRs. utena's `git.prs` job runs at 30s; 60s is
fine. A side benefit over the daemon: each tick is a fresh process, so `gh auth token` is
re-resolved instead of being cached for the daemon's lifetime.

## Session model

**Do not use `herdr worktree open`.** It was the obvious choice and it is wrong. Each call
creates *two* workspaces — the checkout plus a parent-repo workspace — giving 2N+1 per
session. The parent is structural: closing it **cascades and destroys its worktree
children**, and herdr cannot hide a workspace (no `hidden`/`visible` field in the schema, no
sidebar filter). Register each checkout as a plain workspace instead:

```
herdr workspace create --cwd <session-root>/<repo> --label <repo>
```

Plus one for the agent at the session root. That is **N+1**, with no parent rows.

This is safe because **the sidebar's `branch` token reads from the workspace cwd, not the
`worktree` binding** — verified on 0.8.0 by registering an identical worktree as a plain
workspace and watching its distinct branch render. The binding buys no git display we lose.
What N+1 forfeits: `worktree.created`/`worktree.opened` never fire, so
`tdi/herdr-worktree-setup` will not run against our checkouts and `herdr worktree
list/remove` will not manage them. No repo currently uses setup actions, so this costs
nothing today.

## Persistence: the filesystem is the source of truth

There is no central state file. A session **is** a directory:

```
<sessions-root>/<name>/
  .utena-session.json     manifest — owner, name, created_at, archived, repos
  CLAUDE.md               generated, marker-guarded
  .claude/settings.local.json
  <repo-a>/  <repo-b>/    git checkouts
```

Three layers, each holding only what it uniquely can:

1. **Filesystem** — durable truth, survives herdr restarts and plugin reinstalls. The
   colocated manifest is the pattern `herdr-multirepo` uses for `.feature.json`.
2. **herdr** — live state: which workspaces are open, agent status, focus. Matched to
   checkouts by pane cwd, since workspace ids are not durable.
3. **git** — branch and dirtiness, always derived, never stored.

A central `sessions.json` was tried first and caused two bugs: the sidebar pane and the CLI
resolved `HERDR_PLUGIN_STATE_DIR` differently and read different files, and the sidebar
reported zero sessions while 27 existed on disk. Both are duplicate-source-of-truth
failures.

**Archiving is persisted, not derived.** "No live workspaces" cannot mean archived, because
every session looks like that after a herdr restart. `archived: true` lives in the manifest;
the sidebar hides those behind a `.` toggle.

**Discovery must not shell out to git.** A worktree's `.git` is a file pointing at its
gitdir; `HEAD` and `commondir` there give branch and origin repo by plain file read — 83
checkouts in 0.06s. Running `git status --porcelain` per checkout instead took over two
minutes (43 of them exceed 0.5s; the worst is 12s), which hung the sidebar's first load
entirely. Dirtiness is therefore computed asynchronously, for the selected row only. Handle
both layouts: `.git` as a file (worktree) **and** as a directory (an ordinary clone inside a
session), or checkouts get silently dropped.

**Existing utena sessions need no import.** Discovery falls back to the
`<!-- utena:multi-session-guide -->` marker in a generated `CLAUDE.md`, so pre-plugin
sessions appear as soon as their root is scanned.

## Grouping without nesting

herdr cannot nest workspaces on demand: the API schema has no parent, child, group, indent
or depth field anywhere, and nesting derives solely from the `worktree` binding. Two
socket-only calls provide the alternative — neither is exposed on the CLI:

- `workspace.report_metadata` — tag each workspace `session=<name>`; render with a
  `$session` column in `[ui.sidebar.spaces]`.
- `workspace.move_block` — make a session's rows contiguous in one call.

The plugin renders its own tree in a pane instead, which is the only way to get real
nesting, hidden repo rows, and archived-session filtering.

## What we reuse

| Component | Verdict | Notes |
|---|---|---|
| `internal/claudesettings/` | **import as-is** | stdlib only, no DB/HTTP/globals. `EnsureMultiSessionGuide`, `EnsureSessionRoot`, `LinkWorktree`. Change the `<!-- utena:multi-session-guide -->` marker → note that existing generated files then read as user-authored and are never rewritten. |
| `internal/tui/{theme,list,router,filepicker}` | **import as-is** | zero domain deps. |
| `internal/tui/branchpicker` | **import, 1-line tweak** | all-string API already; only consumes `provider.Branches*Msg`. |
| `internal/tui/workspacepicker` | **copy, new data layer** | multi-select UI already exists — `space` toggles, `enter` implicit-selects the row under the cursor. `map[uint]` → string. |
| `internal/tui/sessionform` | **copy, new data layer** | the 6-step machine (`workspacePicker → filePicker → branchPicker → branchManualInput → branchMode → nameInput`) is pure logic; ~8 `provider.X()` call sites change. Its tests drive it purely through messages and port with it. |
| `internal/tui/sessionlist` | **copy, trim** | widest surface — drags in `sessiondetail`, `statusview`, `claude`, `git`. Port the list half; rewrite the detail panel. |
| `internal/tui/provider/` | **not used** | the sidebar was written fresh against the socket rather than porting the provider layer; it needed a tree, ungrouped rows and archived filtering that the session-list views did not have. |
| `internal/git/gitcli.go` | **not used** | discovery reads git metadata from files; only `worktree add`/`remove` shell out. |
| `internal/git/githubclient.go` | **reuse verbatim** | `NewGitHubClient(ctx)` takes only a context. `$GITHUB_TOKEN` → `gh auth token`. |
| `SanitizeSessionName` | **copied (~15 lines)** | pure, but lives in the GORM-laden `session` package. |
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

1. ~~**Skeleton**~~ — manifest, build script, `main.go` dispatch, socket + CLI client.
2. ~~**Session core**~~ — worktrees, `claudesettings`, manifest, create/list.
3. ~~**Sidebar**~~ — Bubbletea tree in a pane, event-driven; create, activate, archive,
   delete; `prefix+u` toggles it, `prefix+p` opens a session-aware picker popup.
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
