# Session Picker: Implementation Gap Analysis

## Status Summary

| Area | Implemented | Remaining |
|------|------------|-----------|
| Daemon API | ~80% | Activate endpoint, persistence, workspace discovery, validation |
| Plugin | ~90% | Hardcoded session ID (Ctrl+P handled in zellij config) |
| TUI | ~10% | All views, API client, view routing |

## Architecture Decision: Workspace Matching

Sessions created through our TUI have explicit workspace info. External sessions discovered via plugin `SessionUpdate` events are stored **without** workspace assignment. The TUI displays them regardless.

## Task Dependency Graph

```
Independent (can parallelize):
  #1 Fix plugin session ID
  #2 Session persistence
  #3 Workspace discovery
  #4 Session name validation
  #6 Handle external sessions (no workspace)

Sequential chain:
  #5 Activate endpoint → #7 API client → #8 Session list view  ─┐
                                        → #9 Workspace picker  ─┤→ #11 App view router
                                        → #10 Name input view ─┘
```
