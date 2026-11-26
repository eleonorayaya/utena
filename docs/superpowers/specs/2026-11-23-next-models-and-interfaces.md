# utenad: Models and Interfaces

Status: high-level design for the `next` branch, which installs as `utenad`. It has no user interface, and everything is unit tested.

## Model

- **Session:** a name plus the hosts it has resources on. It's a record, not an interface.
- **Host:** a place where a session's resources live, either local or remote. Each host provides the session's checkouts and its runtime.
- **Repo:** something a session can check out.
- **Checkout:** a copy of a repo on a host, on one branch at a time.
- **Runtime:** where a session's processes and agents run on a host.
- **Agent:** something working in a runtime. It has an id and an activity (working, idle, finished, blocked, errored, limited or unknown).
- **Pull request:** belongs to a repo and a branch, not to a host or a session.

## Interfaces

Only Host and Runtime are interfaces, because each has real alternatives behind it. Checkouts and pull requests are plain modules whose API is the abstraction.

```go
type Host interface {
    Up()
    Down()
    Available() bool
    Setup(session)
    Delete(session)
    Describe(session) View
}

type Runtime interface {
    Start(session)
    Stop(session)
    Running(session) bool
    LaunchAgent(session) AgentID
    Agents(session) []Agent
    AgentState(AgentID) Activity
    AgentOutput(AgentID) Output
    SendToAgent(AgentID, text)
}
```

- **Host:**
  - Up, Down and Available are about the host itself. The local host is always up.
  - Setup and Delete create and remove the session's resources on the host.
  - Describe returns the session's checkouts, runtime and agents on that host.
- **Runtime:**
  - Start and Stop bring the place where the session's processes run up or down.
  - Agents are launched, listed, read and sent text through the runtime.
  - An agent id is opaque. Only the runtime understands it, and it stays the same for the agent's lifetime.

## Modules

- **Checkouts:**
  - create a checkout of a repo at a branch, and remove one;
  - report its current branch;
  - keep the history of every branch it has been on.
    - The checkout notices when its branch changes. The session isn't involved.
    - The history outlives the checkout, so its PRs can still be found after the checkout is removed.
  - Several checkouts can be on the same branch. The branch belongs to the repo, and each checkout points at it.
- **Pull requests:**
  - read-only;
  - looked up by repo and branch.
  - A session's PRs are found through its checkouts' branch histories.

## How it fits together

```
 session ──► host ──┬─► runtime ──► agents
                    └─► checkouts
                            │ on
                            ▼
              repo ──► branches ──► pull requests
```

- **Starting a session:** bring its host up, then start its runtime there.
- **Stopping a session:** stop its runtime. The host can stay up.
- **State is read live:** nothing is pushed as events. Callers read the host, runtime and checkouts when they need to.

## Not in next

- orchestrators and permissioning;
- attention and holds, which get a redesign of their own;
- any user interface.
