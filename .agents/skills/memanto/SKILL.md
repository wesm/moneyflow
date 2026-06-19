---
name: memanto-memory
description: Use this skill when you need to store or search MEMANTO persistent memories. It defines mandatory guidelines for best practices, memory types, confidence levels, tagging, and patterns for effective agent memory usage.
---

# MEMANTO Memory Skill

Detailed reference for using MEMANTO persistent memory effectively.

## Memory Types: Decision Matrix

| Type | When to Use | Confidence | Example |
|------|-------------|------------|---------|
| `fact` | Verified information, project status | 0.9-1.0 | "MEMANTO uses PostgreSQL for metadata" |
| `decision` | Architecture choices, approach selections | 0.9-1.0 | "Chose React over Vue for frontend" |
| `instruction` | Standing rules, preferences, guidelines | 0.9-1.0 | "Always use type hints in Python" |
| `commitment` | Promises, TODOs, obligations | 1.0 | "Will deploy monitoring by Friday" |
| `preference` | User/team preferences | 0.8-1.0 | "User prefers dark mode" |
| `goal` | Objectives, targets, milestones | 0.8-1.0 | "Launch CLI by end of March" |
| `artifact` | Tool outputs, reports, file locations | 0.9-1.0 | "Report saved at ./reports/q1.md" |
| `learning` | Knowledge acquired from experience | 0.7-0.9 | "Batch operations 100x faster" |
| `event` | Important conversations, milestones | 0.8-0.95 | "Completed Phase 1 features" |
| `relationship` | Team context, collaboration patterns | 0.85-0.95 | "Alice is lead backend engineer" |
| `observation` | Patterns noticed, behaviors | 0.6-0.85 | "User prefers short responses" |
| `error` | Failures, bugs, lessons learned | 0.95-1.0 | "Namespace format bug - use underscores" |
| `context` | Session summaries, status updates | 0.9-1.0 | "Project 70% done, API complete" |

## Confidence Levels

- **1.0** — Explicit user statement, verified fact, standing instruction
- **0.9-0.95** — Strong consensus, well-tested approach, clear team preference
- **0.8-0.85** — Observed pattern (3+ times), indirect but supported preference
- **0.7-0.75** — Emerging pattern (2 times), reasonable inference
- **0.6-0.65** — Single observation, uncertain interpretation
- **< 0.6** — Don't store. Too uncertain.

## Provenance Types

Always categorize the source of the memory. Valid options:
- `explicit_statement` — Directly stated by user
- `inferred` — Derived from behavior/context
- `observed` — Seen in action
- `corrected` — Updated after contradiction
- `validated` — Confirmed/verified
- `imported` — Brought in from an external source (file upload, sync, migration)

## Source Types

Always specify the tool or agent creating the memory.
- For AI agents: Use the agent name (e.g., `--source claude_code` or `--source cursor`)
- Valid base sources (if not using specific agent name): `user`, `agent`, `tool`, `system`

## Tagging Best Practices

Use 2-5 tags per memory. Tags make memories findable.

Good: `--tags "authentication,oauth,security"`
Good: `--tags "bug-fix,namespace,commit-3f39351"`
Bad: `--tags "important"` (too generic)
Bad: `--tags "thing"` (not descriptive)

Conventions:
- Lowercase with hyphens: `bug-fix` not `BugFix`
- Be specific: `authentication-oauth` not `auth`
- Include refs: `commit-abc123` for git references

## Patterns

### Session Start
```bash
# recall — load raw context (instructions, decisions, goals) to guide this session
memanto recall "instructions decisions goals" --limit 20

# answer — get a direct synthesized summary of pending commitments
memanto answer "What are my pending commitments?"
```

### After Important Work
```bash
memanto remember "Implemented X using approach Y because Z. Commit abc123." --type decision --tags "feature-x" --confidence 0.95 --provenance "inferred" --source "claude_code"
memanto remember "Learned that batch ops reduce API calls 100x." --type learning --tags "performance" --confidence 0.85 --provenance "observed" --source "claude_code"
```

### When User Corrects You
```bash
memanto remember "User corrected: prefer pytest over unittest." --type learning --tags "correction,testing" --confidence 1.0 --provenance "corrected" --source "claude_code"
```

### Choosing Between recall and answer

These are **equal-priority tools**. Pick the right one — do NOT always default to `recall`.

| Situation | Use |
|-----------|-----|
| Need raw memory chunks to read and apply as context | `recall` |
| Need a direct synthesized answer to give (or act on) | `answer` |
| Building context before a complex multi-step task | `recall` |
| User asks "what did we decide / prefer / commit to?" | `answer` |
| Comparing multiple matching memories | `recall` |
| Need one grounded yes/no or summary response | `answer` |

**Decision rule**: If your next step is *"read these memories and act"* → `recall`. If your next step is *"answer this question directly"* → `answer`. Both save tokens equally — `answer` synthesizes so you don't have to.

```bash
# Use recall — need raw context to work from
memanto recall "authentication approach" --limit 10

# Use answer — need a direct synthesized answer
memanto answer "What auth approach did we decide on and why?"
```

## Pitfalls to Avoid

1. **Memory hoarding** — Ask "Will this matter in a week?" before storing
2. **Vague content** — Bad: "better performance" → Good: "API response < 200ms"
3. **No context** — Bad: "fixed bug" → Good: "Fixed OAuth expiry bug. Commit abc123."
4. **Duplicates** — Search first (`memanto recall`), then store if not found
5. **Missing tags** — Always include tags for retrieval

## recall vs answer: Choose the Right Tool

**Equal priority** — do NOT always default to `recall`. Pick based on what you need next:

| Use `recall` when... | Use `answer` when... |
|---------------------|---------------------|
| You need raw memory chunks as context | You need one direct synthesized response |
| Building context before a complex task | User asks "what did we decide / prefer?" |
| Comparing or reviewing multiple memories | Getting a grounded summary or yes/no |
| Next step: *read these and act on them* | Next step: *deliver this as the answer* |

**Short rule**: need context to work from → `recall`. Need a ready answer → `answer`. Both save the agent tokens and time — `answer` synthesizes so you don't have to read and merge manually.

## Command Reference

```bash
# Store memory
memanto remember "content" --type TYPE --tags "tag1,tag2" --confidence 0.9 --provenance "inferred" --source "claude_code"

# Raw memory search (use for context-building, multi-step tasks)
memanto recall "query" --limit 10 --type TYPE --min-similarity 0.8

# Temporal recall variants (no query needed)
memanto recall --recent --limit 10                 # newest first
memanto recall --as-of "2026-01-15"                # state at a point in time
memanto recall --changed-since "last 7 days"       # what changed since

# Synthesized answer (use for direct questions, "what did we decide about X?")
memanto answer "question"

# Sync memories to project
memanto memory sync --project-dir .
```
