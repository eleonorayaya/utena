# Orchestration Walkthrough

One orchestrated task followed end to end against the [agent orchestration design](2026-11-14-agent-orchestration-design.md). Markers:

- **[Open]** the design doesn't answer this yet.
- **[Conflict]** two parts of the design pull in different directions.

## Example

You have a long-running orchestrator. You tell it: "the daemon crashes when a coder workspace disappears; get it fixed." It spawns a worker, the worker fixes the bug, and the fix is merged.

## Steps

1. **You give the orchestrator the task.**
   - You type into the orchestrator's agent, as with any session.
   - The orchestrator reads its standing guidance to decide how to handle it.
   - **[Open]** How a session becomes an orchestrator in the first place.

2. **The orchestrator spawns a worker.**
   - It asks the daemon to create a session with a checkout of utena, a runtime and an agent, on a host its guidance picks.
   - The daemon checks the spawn gate (allowed by default) and records the action.
   - The orchestrator now drives the new session.

3. **The worker gets its first brief.**
   - The brief is the worker agent's first prompt: the task, plus what "done" means.
   - The orchestrator goes quiet and waits on its feed.
   - **[Open]** What the orchestrator does while waiting: stays running, or stops and is woken by its feed. If it stops, what starts it again.

4. **The worker works.**
   - The worker agent reads code and makes changes.
   - Its activity (working, finished, blocked) is tracked by utena.
   - The orchestrator's feed shows only milestones, not this activity.

5. **The worker hits a tool-permission prompt.**
   - The agent is blocked and an attention item opens.
   - Answering tool-permission prompts defaults to asking you, so the item is addressed to you, not the orchestrator.
   - You answer it in the utena app, and the worker continues.
   - **[Open]** How the daemon answers the prompt through tuios.

6. **The worker opens a PR.**
   - The PR is tracked on the worker's branch.
   - The orchestrator's feed gets a milestone.

7. **CI fails.**
   - The failure reaches the worker through its session feed.
   - A "PR needs action" attention item opens, addressed to the orchestrator.
   - **[Conflict]** Both the worker (via its feed) and the orchestrator (via attention) learn about the failure and may both act on it. It's unclear who owns the fix.

8. **The worker fixes CI and asks for review.**
   - CI goes green.
   - The worker is now waiting on a reviewer.

9. **The worker is held and stopped.**
   - A hold is set with a wake condition: "a review arrives on the PR".
   - The worker is stopped to save resources. Its conversation, checkout and PR tracking survive.
   - **[Open]** Who sets the hold: the worker, the orchestrator, or either.

10. **A review arrives.**
    - The hold's condition is met and the hold lifts.
    - The worker is started again and its agent resumes its conversation.
    - The session feed delivers the review comments it missed while stopped.
    - **[Open]** Whether the agent can always be resumed from the stored conversation.

11. **You drop into the worker.**
    - You attach to look at the change, or to steer it.
    - You take precedence: the orchestrator doesn't stop the session or type into it while you're there.
    - **[Conflict]** The orchestrator "drives" the session exclusively, yet you can act in it at any time. What the orchestrator may still do while you're present isn't defined.

12. **The work is approved and ready to ship.**
    - The orchestrator decides the brief is done.
    - **[Open]** "Ship" isn't defined: merge only, or merge plus whatever follows (deploy, notes, cleanup).

13. **The orchestrator merges.**
    - It asks the daemon to merge. The merge gate says "ask", so an approval item goes to you.
    - You approve in the utena app, and the daemon merges.
    - **[Open]** How the approval is proven to be from you.

14. **The worker is closed.**
    - The orchestrator archives the worker, or gives it a next brief.
    - Archiving ends management and releases the runtime. The checkout and conversation are kept.
    - The orchestrator's feed records the outcome, and the orchestrator reports back to you.
