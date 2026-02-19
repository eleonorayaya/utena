---
name: taskfile-commands
description: Use when building, running, testing, formatting, deploying, or viewing logs for any component — daemon, TUI, or Zellij plugin
---

# Taskfile Commands

This project uses [Task](https://taskfile.dev) as its build system. NEVER run raw build commands directly. ALWAYS use `task <target>` from the project root.

## Command Reference

All commands run from the project root `/Users/eleonora/dev/utena`.

| Target | What it does |
|--------|-------------|
| `task daemon:build` | Build the daemon binary |
| `task daemon:run` | Build and run the daemon (port 3333) |
| `task tui:build` | Build the TUI binary |
| `task tui:run` | Build and run the TUI |
| `task tui:deploy` | Build and copy TUI to `/usr/local/bin/utena` |
| `task tui:logs` | Tail the Bubbletea log file |
| `task plugin:build` | Build the Zellij plugin (wasm32-wasip1) |
| `task plugin:deploy` | Build, copy to Zellij plugins dir, and reload |
| `task plugin:reload` | Reload the plugin in Zellij |
| `task plugin:logs` | Tail plugin log file |
| `task plugin:logs_tail` | Tail plugin log file (streaming) |
| `task plugin:zellijlogs` | Show recent Zellij logs |
| `task plugin:zellijlogs_tail` | Tail Zellij logs (streaming) |
| `task plugin:fmt` | Format Rust code |
| `task fmt` | Format all code (Go + Rust) |
| `task test` | Run Go tests |
| `task go:test` | Run Go tests |
| `task go:fmt` | Format Go code |
| `task dev` | Open full dev environment in Zellij |

## Common Mistakes

NEVER do any of these:

```bash
# WRONG: running go commands directly
go build -o out/daemon cmd/daemon/main.go
go build -o out/tui cmd/tui/main.go
go test ./...
go fmt ./...

# WRONG: cd-ing into subdirectories to run task
cd zellij-plugin && task build
cd zellij-plugin && cargo build

# WRONG: running cargo directly
cargo build --target wasm32-wasip1 --release
cargo fmt
```

ALWAYS use the namespaced task targets from the project root:

```bash
# CORRECT
task daemon:build
task tui:build
task go:test
task fmt
task plugin:build
task plugin:fmt
```
