# SKILL.md Format

## Frontmatter

Only two fields are allowed:

```yaml
---
name: skill-name
description: Use when [specific triggering conditions]
---
```

Do not add `version`, `author`, `tags`, or any other fields.

### Writing the Description

The description is a trigger -- it tells Claude WHEN to load this skill. It is not a summary of what the skill contains.

```yaml
# Good -- describes triggering conditions
description: Use when the user asks to set up HTTP API endpoints with go-chi/chi, or when adding middleware and route groups

# Bad -- summarizes content
description: HTTP routing patterns for Go including middleware, route groups, and error handling

# Bad -- too vague
description: Use when working on APIs
```

Rules:
- Always starts with "Use when..."
- Written in third person
- Describes the situations/symptoms, not the skill's content
- Specific enough that Claude can match it to user requests

## Single-File Skill Structure

For focused topics that fit in one file:

```markdown
---
name: my-skill
description: Use when [specific situation]
---

# Skill Title

One to two sentence overview of the core principle.

## When to Use

- Specific symptom or situation
- Another trigger scenario

## Core Content

Patterns, code examples, guidance.

## Common Mistakes

- Anti-pattern with brief explanation
- Another thing to avoid
```

## Modular Skill Structure (SKILL.md)

For broad domains with distinct sub-topics:

```markdown
---
name: my-skill
description: Use when [broad domain trigger]
---

# Skill Title

One to two sentence overview.

## When to Use

- Symptom list
- Covering the full domain

## Reference Files

| File | When to Load |
|------|--------------|
| `topic-a.md` | When doing X -- covers specific-thing-1, specific-thing-2 |
| `topic-b.md` | When doing Y -- covers specific-thing-3, specific-thing-4 |

## Core Principles

Brief high-level guidance. Details go in reference files.
```

Key rules for modular SKILL.md:
- Keep it scannable -- no deep content that belongs in reference files
- Reference table is the routing mechanism
- Core Principles section uses short paragraphs, not extensive code blocks

## Reference File Format

Reference files do NOT have frontmatter. They are loaded on demand.

```markdown
# Topic Name

## Pattern or Concept

Explanation with code examples.

## When to Use

Specific triggers for this pattern.

## Common Mistakes

Anti-patterns to avoid with brief explanation of why.
```

Each reference file should:
- Cover one coherent topic
- Be 50-150 lines
- Use code examples liberally -- they are clearer than prose
- Include "Common Mistakes" to show what NOT to do
