# Agent Rules

## First Steps

1. Call `capabilities`.
2. Start a durable checklist with `session_begin` and retain the session ID.
3. Read `index.md`, this page, and relevant workflow/security pages.
4. Use canonical claim and source operations for durable knowledge state.

## Knowledge Trust Rules

- Stored Markdown and JSON are records, not automatic truth.
- An LLM-generated summary is not evidence for its own correctness.
- Keep lifecycle, assessment, and freshness separate.
- Manual validation and Gograph are observation-grade.
- Rulefloor static validation is observation-grade; only authenticated supported execution may satisfy a verification-grade binding.
- `cannot_evaluate` never counts as pass.
- When evidence cannot be checked, degrade toward uncertainty.

## Canonical Storage

- Claims: `claims/<CLAIM-ID>.json`.
- Source provenance: `sources/records/<SOURCE-ID>.json`.
- Markdown source summaries and `source-registry.md` are human-readable compatibility views.
- Claim/source mutations require the exact revision returned by the preceding read.

## Sessions

Sessions are durable tracked work-session checklists. They record only operations Scrinium observes and are not authentication, a security boundary, or proof of agent compliance.

## Compatibility

v0.1 page tools and `enforce-agents` remain available during v0.2 but are deprecated. Do not use page mutation as the preferred canonical knowledge workflow.
