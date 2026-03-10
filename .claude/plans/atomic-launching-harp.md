# Plan: Add "Never Swallow Errors" guidance to go-patterns skill

## Context

User wants to codify the rule that errors must never be silently swallowed. The `error-handling.md` file in the go-patterns skill is the right place — it already covers error definition patterns and controller-level handling but lacks this fundamental principle.

## Change

**File**: `.claude/skills/go-patterns/error-handling.md`

Add a new section `## Never Swallow Errors` immediately after line 1 (before "## Sentinel Errors"), establishing it as the first and most fundamental rule.

Content to add:

```markdown
## Never Swallow Errors

Every error must be explicitly handled -- never discard with `_` or ignore a return value.

```go
// Wrong: silently swallowed
result, _ := doSomething()
doSomething() // ignoring error return

// Correct: always handle
result, err := doSomething()
if err != nil {
	return fmt.Errorf("doing something: %w", err)
}
```

Logging an error and returning nil is also swallowing -- the caller loses the ability to react. Never do this unless the user explicitly asks for it.

```go
// Wrong: swallowed via log-and-discard
if err != nil {
	log.Printf("failed: %v", err)
	return nil // caller has no idea something failed
}

// Correct: propagate the error
if err != nil {
	return fmt.Errorf("doing something: %w", err)
}
```
```

## Verification

- Read the modified file to confirm the section is properly placed and formatted
- Confirm the skill index (`go-patterns` main file) already references `error-handling.md` for error handling guidance (it does)
