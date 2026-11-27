# Agent Orchestration

Status: high-level design of the desired end state. The utena app (your UI, a standalone app outside the mux) gets its own design session.

## Goal

Start an orchestrator agent in utena and let it run other sessions on its own: spawn workers, brief them, watch them, ship their work, and put them away, within the guidance you give it. It is a long-lived manager across unrelated work, not a conductor for one task.

## Requirements and assumptions

Requirements:

- An orchestrator can spawn, brief, watch, ship and close sessions without you, within your guidance.
- Management is opt-in: an orchestrator touches only sessions it spawned or was handed.
- At most one orchestrator drives a session at a time, and you always take precedence over it.
- Gates decide, per kind of action, whether an orchestrator may act, must ask you, or may not. Only you can loosen them.
- Every caller is an authenticated principal limited to its own permissions. Orchestrators act only through the daemon, and every action is recorded.
- Some actions need you personally, approved in a way agents can't fake.
- Stopping a session loses nothing durable: its conversations and feed survive, and its agents can resume.
- Each decision that needs someone goes to exactly one resolver.

Assumptions:

- The runtime runs agents and gives access to them.
- One leader daemon runs orchestration; coder workspaces run follower daemons.
- You make approvals in the utena app.

## Core ideas

| Topic | Decision |
|---|---|
| Autonomy | Autopilot by default, shaped by your guidance. Default gates allow most actions; answering a worker's tool-permission prompt defaults to asking you |
| Orchestrators | Any number, as flat peers |
| Management | An orchestrator either drives a session (exclusive among orchestrators) or watches it |
| Attention | utena owns what needs attention and who it's for |
| Trust | Every API caller is an authenticated principal. Orchestrators act only through the daemon |
| Tool surface | A CLI over the API, plus a skill. No MCP server |

## Sessions and resources

A session is an identity plus the resources it owns. Each resource kind belongs to its own module and tracks its own state and health; the session's health is a rollup.

- **Hosts.** Your machine or a coder workspace; a session can use several. Agents run in their host's runtime and work on checkouts on the same host. Browser windows open wherever you are. A coder workspace's resources are run by its follower daemon, which reports to the leader.
- **Runtime.** Each host the session uses has at most one runtime, where its agents and shells run. The runtime can be a multiplexer, which you can also attach to. A session with no runtime up on any host is stopped, and nothing durable is lost.

| Resource | Per session |
|---|---|
| Runtime | 0..1 per host |
| Checkout | 0..n, at most one per repo |
| Agent | 0..n |
| Browser window | 0..n |
| Tracked branch (and its PRs) | 0..n, outlives checkouts |

Sessions gain and lose resources and hosts over time; any resource may be absent. Modules plug their resources into sessions through small shared interfaces rather than `session` knowing about each one.

## Agents

The runtime that runs an agent understands what it is doing and how to talk to it. utena tracks each agent's activity (working, finished, blocked on a prompt, and so on) and its conversation, so an agent can be stopped and resumed later. utena, not the runtime, is the record.

## Three channels

- **Session feed:** what the working agent should hear, such as review comments, CI results and messages from its orchestrator. Durable, so nothing is lost while a session is stopped.
- **Attention:** decisions that need someone. Each item is about a specific resource and is addressed to whoever may resolve it: the driving orchestrator if its gates allow, otherwise you.
- **Orchestrator feed:** a coarse, session-level view for managers: the attention items addressed to it, plus milestones, not raw events.

## Trust

- Every caller is a principal: you, a session, an orchestrator, another daemon, the browser extension. Each is limited to its own permissions.
- Some actions need you personally. You approve them in the utena app, in a way agents can't fake.
- tuios trusts only the daemon.

## Orchestration

- **Orchestrator:** a session marked as one, with standing guidance.
- **Worker:** a session an orchestrator drives.
- **Workstream:** an optional named group of managed sessions with its own guidance.
- **Brief:** one unit of work given to a worker's agent. A worker can take many briefs over its life.
- **Gates:** for each kind of action, whether the orchestrator may do it, must ask you, or may not. Your guidance sets them; loosening needs your approval.

Every orchestrator action is recorded.

## Worker lifecycle

```
 spawn ─► brief ─► work ─► ship ─┬─► next brief
                    ▲            │
                    └─ held + ◄──┴─► close
                       stopped
```

Any session waiting on something outside itself can be **held**, optionally with a condition that wakes it. A held session stays quiet. A held worker is stopped to save resources, then resumed when the hold lifts or a new brief arrives. If you're in a session, you take precedence over its orchestrator.

## Module layering

```
 orchestrator
      │
 attention
      │
 session
      │
 git · mux · agent · browser · coder
      │
 auth · eventbus · db · common
```

Lower modules never import upper ones. The shared interfaces that resource modules implement are defined low enough to keep that true.

## Open questions

- How the person proof works in the utena app.
- How tuios lets the daemon answer agent prompts.
- Whether an orchestrator can manage another orchestrator.
