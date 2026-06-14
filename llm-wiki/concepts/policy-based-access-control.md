---
title: Policy-Based Access Control
type: concept
status: current
updated: 2026-06-13
sources:
  - SRC-20260613-project-design
---

# Policy-Based Access Control

## Definition

Policy-Based Access Control, or PBAC, is Scrinium's deterministic governance layer for deciding whether a wiki write is allowed. The policy is configured through `scrinium.json`, including protected file patterns such as foundational rules, architecture pages, and decision records. Source: `SRC-20260613-project-design`.

## Why It Matters

The LLM Wiki pattern depends on agents being able to maintain a wiki over time, but project rules and architecture records can decay if every page is freely mutable. PBAC lets agents update working knowledge while protecting foundational context from accidental or unauthorized overwrite. Source: `SRC-20260613-project-design`.

## Evidence and Examples

- `scrinium.json` configures `wiki_root` and `write_governance.protected_files`. Source: `SRC-20260613-project-design`.
- Current protected zones include `rules.md`, `architecture/*`, and `core-decisions/*`.
- `agent-rules.md` is intentionally writable in the active project config so the agent schema can evolve directly.
- Mutable zones include drafts, working notes, agent rules, generated wiki pages, and logs.

## Related Concepts

- `concepts/semantic-rejection.md`
- `projects/scrinium.md`

## Open Questions

- Whether future Scrinium versions should expose more granular policy types beyond protected file patterns.
- Whether delete operations should remain absent from the agent-facing tool surface despite the source's broad CRUD wording.
