# UI Standards

These apply to any client of the daemon. The code standards in `code-standards.md` also apply.

- A client talks to the daemon only through its HTTP API and websockets, using one API client package.
- The screen is built from child components. Each one owns its own state, input handling and rendering, and the parent only routes to them.
- Each component defines its keybindings in one keymap, and the help view is generated from it.
- Updates arrive over the daemon's websocket. After a reconnect, the client re-syncs the full state.
- Views get data through providers that wrap the API client. Tests fake the providers.
- The UI holds no business rules. If the UI needs a rule, the daemon exposes it.
