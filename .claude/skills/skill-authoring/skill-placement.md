# Skill Placement

## Global vs Project Skills

| Location | Scope | Version Controlled |
|----------|-------|-------------------|
| `~/.claude/skills/` | All projects | No (user-local) |
| `project/.claude/skills/` | Project only | Yes (with project) |

## When to Use Global Skills

Place in `~/.claude/skills/` when:
- Pattern applies across multiple projects
- Skill is a personal workflow preference
- Content is language/framework agnostic

Examples: git workflow patterns, code review checklists, debugging methodology.

## When to Use Project Skills

Place in `project/.claude/skills/` when:
- Pattern is specific to this codebase
- Team should share the knowledge
- Skill references project-specific files or conventions

Examples: project architecture patterns, codebase-specific conventions, team coding standards.

## Moving Skills Between Scopes

### Global to Project

```bash
mv ~/.claude/skills/my-skill project/.claude/skills/
```

When the skill evolved to be project-specific, or you want to version control it, or share it with the team.

### Project to Global

```bash
mv project/.claude/skills/my-skill ~/.claude/skills/
```

When the pattern generalized beyond one project, or you want it available everywhere, or it is a personal preference rather than a team standard.

## Directory Layout

```
~/.claude/
  skills/
    git-workflow.md           # Single-file global skill
    debugging/                # Modular global skill
      SKILL.md
      memory-leaks.md
      race-conditions.md

my-project/
  .claude/
    skills/
      architecture.md         # Single-file project skill
      api-conventions/        # Modular project skill
        SKILL.md
        endpoints.md
        error-codes.md
```

## Common Mistakes

- Putting project-specific patterns in global skills (they drift from the codebase)
- Duplicating a skill across multiple projects instead of extracting the general version to global
- Forgetting that global skills are not version controlled and not shared with the team
