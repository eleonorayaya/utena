# Browser eject and attached-session links

## Problems

1. Ctrl+clicking a link in kitty opens it in the browser's last focused
   window, not the attached session's window. kitty uses
   `open_url_with default`, so links go through `/usr/bin/open` and never
   reach utena. kitty runs outside tuios, so it has no `UTENA_SESSION_ID`.
2. There is no way to release a browser window from a session without
   closing it.

## Attached-session links

- `OpenRequest` gains `attached` (bool). When `session_id` is 0 and
  `attached` is set, `Service.Open` resolves the attached session
  (`Sessions.Attached`) and opens the link as that session. With no attached
  session it is a session-0 link, as before. A non-zero `session_id` wins.
- `utena open --attached <url>...` sets it when the environment has no
  session ID. The `utena-open` alias passes flags through.
- kitty (dotfiles): `open_url_with ~/.local/bin/utena-open --attached`. When
  the daemon is down, `utena open` falls back to `/usr/bin/open`.

## Eject

- `POST /browser/eject {session_id, window_id}` → `{window_id, moved}`.
- `Service.Eject` requires the window to be an open row owned by the
  session and live in the browser. Under the service lock it resolves the
  window's wm window, deletes the row, cancels its pending snapshot and
  removal timers, and, when the wm window is not on the browser workspace,
  moves it there (`moved`).
- The window is then unowned: placement ignores it, it is not adopted again,
  and session-0 links may use it. The session gets no replacement window
  until its next link or attach, as when a window is closed by hand.
  Pending tabs on the row are dropped.
- TUI: `e` in the session detail panel's Browser section ejects the window
  of the selected tab and refreshes browser state.
