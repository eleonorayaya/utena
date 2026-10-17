# MCP Servers in Bundles Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Bundles can list MCP servers; utena writes the selected ones into the session root's `.mcp.json` and approves them; shizuku owns the definitions and removes them from user scope.

**Architecture:** See `docs/plans/2026-10-17-mcp-bundles-design.md`. utena: catalog gains server definitions (`json.RawMessage`) and per-bundle `mcp_servers`; new `claudesettings.ApplyMcpServers`; session writes plugins and servers together. shizuku (separate repo `../dotfiles`, module `github.com/eleonorayaya/shizuku`): `app.Bundle` type, server definitions in data, daemon.json output, user-scope removal, secrets env line.

**Conventions:** utena: `task` only (`GOFLAGS=-count=1 task test --force`, `task fmt`, `task lint`; only the pre-existing `internal/db/dbservice.go:69` lint finding is acceptable). dotfiles: `task test --force -- -count=1`, `task lint`. Never run `go` directly. No code comments. `sed`/`find` are blocked by hooks. TDD. Commit per task; message ends with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`. Don't name locals `bundles` where the package is imported.

---

### Task 1 (utena): catalog with MCP servers

**Files:** `internal/bundles/catalog.go`, `catalog_test.go`, `internal/api/config.go`, `config_test.go`, `internal/api/app.go`, `internal/api/bundles_test.go`, `internal/session/sessionservice.go` (`SetBundleCatalog` signature only).

- `Bundle` gains `McpServers []string \`json:"mcp_servers,omitempty"\``; `Plugins` becomes `omitempty` too.
- New `type Servers map[string]json.RawMessage`.
- `Catalog` methods:
  - `ManagedServers() []string` — sorted unique server names listed in any bundle.
  - `ResolveServers(names []string) (servers []string, unknown []string)` — sorted unique server names of the known bundles; `unknown` = unknown bundle names (same semantics as `Resolve`).
  - `Entries()` — `Entry` gains `McpServers []string \`json:"mcp_servers"\`` (non-nil copy, like Plugins).
- `api.daemonFile` gains `McpServers bundles.Servers \`json:"mcp_servers"\``; `Config.McpServers bundles.Servers`.
- `SessionService.SetBundleCatalog(catalog bundles.Catalog, servers bundles.Servers)`; store both (field `mcpServers bundles.Servers`). Update the `buildApp` call and every test call site (grep `SetBundleCatalog(`; pass `nil` servers where irrelevant).
- Tests: parse `mcp_servers` from daemon.json (`config_test.go`), `ManagedServers`/`ResolveServers`/`Entries` (`catalog_test.go`), `GET /bundles` includes `mcp_servers` (`bundles_test.go`).
- Commit `feat(bundles): MCP servers in the bundle catalog`.

### Task 2 (utena): `claudesettings.ApplyMcpServers`

**Files:** `internal/claudesettings/claudesettings.go`, `claudesettings_test.go`.

```go
func ApplyMcpServers(rootPath string, managed []string, selected map[string]json.RawMessage) error
```

- `.mcp.json` at `rootPath`: read raw map (missing file + empty `selected` → do nothing for this file; JSON `null` → empty map; malformed → error `parse .mcp.json: ...`). `mcpServers` map: delete every `managed` name, then set each `selected` name to its definition (decode the RawMessage into `any` so output is re-indented). Drop `mcpServers` when empty. Marshal with 2-space indent + newline; skip write when bytes unchanged; write `0o644`.
- `settings.local.json` (`.claude/` under root): same read/skip rules as `ApplyPlugins`. `enabledMcpjsonServers` is a JSON array of strings: remove managed names, append selected names (sorted), keep other entries in order. Drop the key when empty. Skip unchanged.
- Factor the read-raw / write-if-changed logic shared with `ApplyPlugins` into small helpers if it removes duplication; keep `ApplyPlugins` behavior identical (its tests must still pass).
- Table tests mirroring the `ApplyPlugins` ones: selected written to both files; managed-unselected removed; unmanaged server and unmanaged approval untouched; nothing selected + no files → no files created; empty after removal → keys dropped; malformed `.mcp.json` → error; null file; unchanged → mtime/bytes unchanged (follow existing test style).
- Commit `feat(claudesettings): write bundle MCP servers to .mcp.json`.

### Task 3 (utena): session writes servers

**Files:** `internal/session/sessionservice.go`, `sessionservice_bundles_test.go`, `sessionrouter_test.go` if needed.

- Rename `writePlugins` → `writeBundles`. After `ApplyPlugins`, resolve servers: `names, _ := s.bundleCatalog.ResolveServers(sess.Bundles)`; build `selected` from `s.mcpServers`; names missing a definition → `slog.WarnContext(..., "bundle references unknown MCP servers", ...)` and skip. Call `ApplyMcpServers(sess.SessionRoot, s.bundleCatalog.ManagedServers(), selected)`.
- Return the plugin and server errors separately: `writeBundles` returns `(pluginsErr, mcpErr error)`. `applyClaudeSettings` emits up to two warnings, `claude-plugins: <err>` and `claude-mcp: <err>`. `SetBundles` wraps them as `plugins: %w` / `mcp: %w` and returns their `errors.Join`. `ActivateSession` / `AddWorkspace` keep warning-only behavior; `SetBundles` still returns the error.
- Tests (extend `sessionservice_bundles_test.go`; extend `testBundleCatalog` with a bundle `"homelab": {McpServers: ["ha"]}` and pass servers `{"ha": {"type":"http","url":"https://x"}}`): SetBundles(["homelab"]) writes `.mcp.json` with `ha` and `enabledMcpjsonServers: ["ha"]`; SetBundles([]) removes both; a bundle naming an undefined server writes nothing for it and doesn't error.
- Commit `feat(session): write bundle MCP servers per session`.

### Task 4 (utena): TUI

**Files:** `internal/tui/bundlepicker/item.go`, `bundlepicker.go` (entries carry servers), tests; `internal/tui/sessiondetail/model.go`, `model_test.go`.

- `bundleItem` gains `servers []string`; detail text = plugins joined with `, ` followed by each server as `name (mcp)`, same muted style. `entries()` copies `e.McpServers`.
- Session detail save status: `bundles saved · run /reload-plugins and /mcp in open Claude panes`.
- Tests: picker row shows `zoo (mcp)`; existing hint test checks `/mcp` too.
- Commit `feat(tui): show MCP servers in bundles`.

### Task 5 (dotfiles): shizuku

**Files:** `app/` (new `bundle.go`), `agents/claude/claude.go`, `globalconfig.go`, tests; `programs/utena/utena.go`, test; `examples/eleonora/data/bundles.go`, `claude.go`, `main.go`.

- `app.Bundle{Plugins []string; McpServers []string}`.
- `claude.Options.Bundles map[string]app.Bundle`; `collectBundledPlugins` reads `.Plugins`. New `claude.Options.McpServers map[string]map[string]any`.
- `globalconfig.go`: `removeGlobalMcpServers(path string, names []string) error` — read `~/.claude.json`, delete names from top-level `mcpServers`, write atomically (same tmp+rename as `mergeGlobalConfig`) only if something changed; no-op when names empty or file missing. Call it in `Sync` with the sorted keys of `McpServers`.
- `Env()` adds a `PreInitScripts` entry: `[ -f "$HOME/.config/shizuku/secrets.env" ] && { set -a; . "$HOME/.config/shizuku/secrets.env"; set +a; }`.
- `utena.Options.Bundles map[string]app.Bundle`, `McpServers map[string]map[string]any`; `daemonConfig` writes `bundles` as `{name: {"plugins": [...], "mcp_servers": [...]}}` (omit empty lists) and `mcp_servers` when non-empty.
- Data: `Bundles()` (rename from `PluginBundles`) returns `map[string]app.Bundle` with the existing plugin bundles plus `homelab` (`McpServers: homeassistant`) and `cad` (`zoo`, `bambuddy-local`); `McpServers()` returns the three definitions from the design doc. Wire `McpServers()` into `ClaudeOptions()` and both `utena.New` calls.
- Tests: claude bundled plugins test updated to `app.Bundle`; `removeGlobalMcpServers` (removes listed, keeps others and other top-level keys, no write when unchanged); `Env()` includes the secrets line; utena daemon.json test updated with servers and `mcp_servers`.
- Verify with `task run -- diff` (no sandbox) that daemon.json gains `mcp_servers` and settings.json is unchanged apart from expected.
- Commit `feat(claude): MCP servers in bundles, secrets env file`.

### Task 6: docs

- utena `README.md` "Plugin bundles": MCP servers, `.mcp.json`, `enabledMcpjsonServers`, `${VAR}` secrets, `/mcp`. `CLAUDE.md` bundles line mentions MCP servers.
- dotfiles `CLAUDE.md` "Managing Shared Claude Code Settings": bundles, MCP servers removed from user scope, `secrets.env`, `DisableClaudeAiConnectors`.
- Commit in each repo `docs: MCP servers in bundles`.
