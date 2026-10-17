# MCP Servers in Plugin Bundles — Design

Date: 2026-10-17

Extends `2026-10-16-plugin-bundles-design.md`: bundles can also turn MCP servers on per session.

## Problem

Claude Code can only turn a user-scope MCP server **off** per project (`/mcp`, stored in `~/.claude.json` `projects[...].disabledMcpServers`). There is no per-project switch to turn one **on**. So "off by default, on per session" can't be done by toggling user-scope servers.

## Decisions

- **Definitions move out of user scope.** shizuku owns them and writes them into utena's `daemon.json`. utena writes the selected servers into the session root's `.mcp.json` (project scope). Non-utena sessions don't see them.
- **Secrets are `${VAR}` placeholders.** Claude Code expands them from its environment at launch. For now shizuku's shell env sources an untracked `~/.config/shizuku/secrets.env` (with `set -a`) if it exists. A password-manager abstraction is a separate, later task.
- Rejected: editing `~/.claude.json` `disabledMcpServers` per project. Servers would stay on everywhere else, and running Claude processes rewrite that file constantly.
- claude.ai connectors are account-level and out of scope here (a separate per-profile shizuku switch, `disableClaudeAiConnectors`, already landed).

## Catalog

```json
{
  "mcp_servers": {
    "homeassistant":  { "type": "http", "url": "https://homeassistant.home.tennyson.io/api/hass_mcp",
                        "headers": { "Authorization": "Bearer ${HOMEASSISTANT_TOKEN}" } },
    "zoo":            { "type": "stdio", "command": "uvx", "args": ["zoo-mcp"] },
    "bambuddy-local": { "type": "stdio", "command": "uvx",
                        "args": ["--with", "mcp==1.30.0", "bambuddy-mcp@0.2.0"],
                        "env": { "BAMBUDDY_URL": "${BAMBUDDY_URL}", "BAMBUDDY_API_KEY": "${BAMBUDDY_API_KEY}" } }
  },
  "bundles": {
    "homelab": { "mcp_servers": ["homeassistant"] },
    "cad":     { "mcp_servers": ["zoo", "bambuddy-local"] }
  }
}
```

Definitions are opaque to utena (`json.RawMessage`) and copied verbatim. A bundle may hold plugins, servers or both. A server name with no definition is logged and skipped.

## Writing per session

Same moments as plugins (setup, add workspace, activate, bundle edits). Managed servers are the names listed in any bundle.

| File | Selected | Managed, not selected | Not managed |
|---|---|---|---|
| `<root>/.mcp.json` `mcpServers` | definition written | key removed | untouched |
| `<root>/.claude/settings.local.json` `enabledMcpjsonServers` | name added | name removed | untouched |

`enabledMcpjsonServers` pre-approves the project servers so Claude doesn't prompt. No file is created when nothing is selected; empty `mcpServers` / `enabledMcpjsonServers` keys are dropped; unchanged files aren't rewritten. Errors surface as `claude-mcp:` setup warnings; `PUT /sessions/{id}/bundles` returns them as errors.

## shizuku

- `app.Bundle{Plugins, McpServers []string}` replaces `map[string][]string`.
- `claude.Options.McpServers map[string]map[string]any`; on sync the claude app deletes those names from `~/.claude.json` `mcpServers` (user scope) and adds the `secrets.env` pre-init line to the shell env.
- `utena.Options.McpServers`; `daemon.json` gets `mcp_servers` and each bundle's `mcp_servers`.
- Data: `homelab` = homeassistant, `cad` = zoo + bambuddy-local. Workspace defaults: homelab-core → homelab, cad-projects → cad.
- Dropped by hand: user-scope `bambuddy` (remote) and `notion` (the docs bundle's plugin ships its own).

## TUI

The bundle picker's detail column lists plugins and servers (`zoo (mcp)`). The save hint becomes "run /reload-plugins and /mcp in open Claude panes".

## Testing

- `claudesettings.ApplyMcpServers`: selected written, unselected managed removed, unmanaged untouched, no file created when empty, malformed `.mcp.json` errors, approvals mirror.
- Catalog/daemon.json parsing of `mcp_servers`; resolve of servers and unknown names.
- Session: create/activate/SetBundles write `.mcp.json`.
- shizuku: daemon.json contents, user-scope removal from `~/.claude.json`, env pre-init line.
