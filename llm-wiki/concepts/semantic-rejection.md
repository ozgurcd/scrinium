---
title: Semantic Rejection
type: concept
status: current
updated: 2026-06-13
sources:
  - SRC-20260613-project-design
---

# Semantic Rejection

## Definition

Semantic rejection is Scrinium's pattern for denying unsafe or unauthorized operations with an explanatory, LLM-readable message instead of a generic failure. The message should explain what was blocked and what safe action the agent should take instead. Source: `SRC-20260613-project-design`.

## Why It Matters

Agents need actionable feedback when governance blocks an operation. A semantic rejection creates a self-correcting loop: the agent learns that a target is protected and can redirect the proposed change to an allowed draft or append-only path. Source: `SRC-20260613-project-design`.

## Evidence and Examples

- If an agent tries to modify a protected architecture page, Scrinium should reject the write and guide the agent toward a draft. Source: `SRC-20260613-project-design`.
- Current wiki governance uses `create_draft` for protected zones and `append_log` for append-only records. Source: `agent-rules.md`, `index.md`, and `SRC-20260613-project-design`.

## Related Concepts

- `concepts/policy-based-access-control.md`
- `projects/scrinium.md`

## Open Questions

- How detailed rejection messages should be without leaking unnecessary policy internals.
- Whether semantic rejection messages should include suggested draft filenames consistently.
