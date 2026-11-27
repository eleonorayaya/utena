# utenad: Models and Interfaces

Status: high-level design for the `next` branch, which installs as `utenad`. It has no user interface, and everything is unit tested.

## Model

- **Session:** a named piece of work.
- **Host:** a machine where sessions' checkouts and runtime live, either local or remote. Sessions share a host.
- **Host session:** a session's presence on one host: which repos it wants there, at most one checkout per repo, each with a starting branch.
- **Repo:** something a session can check out.
- **Checkout:** a copy of a repo on a host's filesystem.
- **Runtime:** where sessions' processes and agents run on a host. Each host has one.
- **Agent:** something working in a runtime, with an activity such as working, blocked or finished.

## Ownership

- Each module owns its records. A record that belongs to something holds its owner's id, and it's looked up through its own module by that id.
- Operations on a session's parts take the session id. Each module removes what it owns for that id, and asks its children to do the same.
- Stored records live centrally. Anything on a host's machine is read live from the host, and nothing is pushed as events.

## Modules

```
session/
  Session  { ID, Name }

  SessionService
    Create(name string) (Session, error)
    Get(id string) (Session, error)
    List() ([]Session, error)
    Start(id string) error
    Stop(id string) error
    Delete(id string) error

host/
  HostSession  { SessionID, Host string, Repos []RepoWant }
  RepoWant     { RepoID, StartBranch }

  HostService
    Add(sessionID, host string, repos []RepoWant) (HostSession, error)
    ForSession(sessionID string) ([]HostSession, error)
    StartSession(sessionID string) error
    StopSession(sessionID string) error
    DeleteSession(sessionID string) error

  Host interface
    Name() string
    Up() error
    Down() error
    Available() bool
    Runtime() runtime.Runtime
    Setup(hs HostSession) error
    Delete(hs HostSession) error
    Describe(hs HostSession) ([]checkout.Checkout, error)

  Hosts
    Get(name string) (Host, error)
    List() []Host

localhost/
  LocalHost implements host.Host
    New(root string, repos repo.RepoService, checkouts checkout.CheckoutService, rt runtime.Runtime) *LocalHost

repo/
  Repo  { ID, Name, Source }

  RepoService
    Add(name, source string) (Repo, error)
    Get(id string) (Repo, error)
    List() ([]Repo, error)
    Remove(id string) error

checkout/
  Checkout  { RepoID, Path, Health }

  CheckoutService
    Create(r repo.Repo, branch, path string) error
    Remove(path string) error
    Inspect(path string) (Checkout, error)

runtime/
  AgentID    opaque string
  Activity   working | blocked | finished | ...
  AgentSpec  { Agent, Prompt, Settings }
  Agent      { ID AgentID, Activity Activity }
  Output     { Text }

  Runtime interface
    Start(sessionID string) error
    Stop(sessionID string) error
    Running(sessionID string) (bool, error)
    LaunchAgent(sessionID string, spec AgentSpec) (AgentID, error)
    Agents(sessionID string) ([]Agent, error)
    AgentOutput(id AgentID) (Output, error)
    SendToAgent(id AgentID, text string) error

tuios/
  implements runtime.Runtime, built by the host with its sessions root

db/
  stores Session, HostSession and Repo
```

- **Host:**
  - Down never loses what's on disk.
  - Setup creates the missing checkouts and never changes existing ones, so it's safe to run again.
  - Describe returns the checkouts, each with its health.
  - The host owns the filesystem, so it decides where a session's files go and tells its runtime.
- **Runtime:**
  - LaunchAgent takes which agent to run, its first prompt and its settings.
  - An agent id is opaque. Only the runtime understands it.

## Dependencies

Each module depends only on modules below it. Interfaces sit below their implementations, and records point up only by id.

```
 app (wiring)
   │
 session
   │
 host ◄──────── localhost
   │               │
   ├─► runtime ◄───┼──── tuios
   │               │
   └─► checkout ◄──┤
         │         │
        repo ◄─────┘
   │
 db
```

## Flows

- **Start a session:** session calls `StartSession`. For each host session, the host module brings the host up, runs Setup, then starts the runtime.
- **Stop a session:** session calls `StopSession`. The host module stops the runtime on each host, and brings a host down when no session on it is running.
- **Delete a session:** session calls `DeleteSession`. The host module calls Delete on each host and removes its host sessions. Then session removes its own record.

## Open

- How callers reach the daemon. With no user interface, it needs an API.

## Not in next

- remote hosts: the design allows for them, but v1 has only the local host;
- orchestrators and permissioning;
- attention and holds;
- pull requests, including auto-review;
- the session feed;
- repo setup, such as copying files and installing dependencies;
- resuming agents;
- browser windows;
- any user interface.
