---
name: skill-authoring
description: Use when the user asks to create, restructure, or organize a Claude Code skill, or when extracting reusable patterns into skill files from retrospectives or session learnings
---

# Skill Authoring

Skills are structured knowledge files that teach Claude Code domain-specific patterns. A well-authored skill loads only what is relevant and guides Claude through code examples, not prose.

## When to Use

- User asks to create a new Claude Code skill
- Extracting patterns from a retrospective into reusable skill files
- Refactoring a monolithic skill into a modular directory structure
- Deciding whether a skill should be single-file or multi-file
- Choosing between global (~/.claude/skills/) and project (.claude/skills/) placement
- Writing or improving reference table descriptions

## Reference Files

| File | When to Load |
|------|--------------|
| `skill-md-format.md` | When writing SKILL.md content -- covers frontmatter rules, section structure, single-file vs modular templates, and reference file format |
| `reference-tables.md` | When creating or improving reference tables -- covers "When to Load" descriptions, file organization strategies, and split-vs-combine decisions |
| `skill-placement.md` | When deciding where a skill lives -- covers global vs project scope, when to move skills, and directory layout examples |

## Core Principles

### Single-File vs Directory

Use a single `.md` file when the topic is focused and fits in one readable file. Use a directory with `SKILL.md` + reference files when there are distinct sub-topics that are independently useful and loading everything would waste context.

### Description is a Trigger, Not a Summary

The frontmatter `description` field determines when Claude loads the skill. It must start with "Use when..." and describe the situations that should trigger it -- not summarize what the skill contains.

### Reference Files Save Context

In modular skills, the root SKILL.md stays scannable with high-level guidance. Detailed patterns, code examples, and anti-patterns go in reference files that load on demand via the reference table.

### Skills Emerge from Experience

The strongest skills come from extracting patterns after completing real work. The flow is: do the work, write a retrospective, identify generalizable patterns, extract into skill files.
