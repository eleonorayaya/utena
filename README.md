# Utena

A workspace management system for Zellij, consisting of a daemon API server, TUI client, and Zellij plugin.

## Documentation

### Architecture & Patterns

- [Architecture Overview](docs/architecture.md) - System components, module dependencies, and communication patterns
- [Module Structure](docs/module-structure.md) - Standard module layout, layer responsibilities, and lifecycle hooks
- [Event Bus Pattern](docs/event-bus.md) - When and how to use events for module communication

### Development

- [Adding Features](docs/adding-features.md) - Step-by-step guides for adding endpoints, events, and modules

### Project Setup

#### Install

```bash
task tui:deploy      # builds and copies to /usr/local/bin/utena
task daemon:build    # builds daemon to ./out/daemon
task plugin:deploy   # builds and deploys the Zellij plugin
```

#### Shell initialization

Add to your `.zshrc` (or `.bashrc`):

```bash
eval "$(utena shell-init)"
```

This sets up environment variables needed for utena integrations (like Claude Code status tracking) when running inside Zellij.

#### Claude Code integration

Utena can track Claude Code session status (working, needs attention, completed) and display it in the TUI.

1. Install the `utena-claude` plugin in Claude Code — point it at this repo's `.claude-plugin/marketplace.json`
2. Ensure `eval "$(utena shell-init)"` is in your shell profile (see above)
3. Start the daemon and use Claude Code inside a Zellij session managed by utena — the TUI will show Claude's status next to each session

## Libraries

### Zellij
- [Zellij Plugin API](https://zellij.dev/documentation/plugin-api.html)
- [Zellij Tile Rust Crate](https://docs.rs/zellij-tile/latest/zellij_tile/index.html)

### Bubble Tea
- [Examples](https://github.com/charmbracelet/bubbletea/tree/main/examples)

See [CLAUDE.md](CLAUDE.md) for build commands and development workflow.