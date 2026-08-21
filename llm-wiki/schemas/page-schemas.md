# Page Schemas

Use these schemas to keep the wiki consistent.

## Common Frontmatter

```yaml
title: <human readable title>
type: source | entity | concept | project | decision | synthesis | status | workflow | schema | security
status: current | draft | superseded | archived
updated: YYYY-MM-DD
sources:
  - SRC-YYYYMMDD-slug
```

Frontmatter is recommended for new generated wiki pages. Preserve existing style unless migration is explicitly requested.

## Canonical Source Record

Path: `sources/records/SRC-YYYYMMDD-slug.json`

Strict schema `scrinium.source/v1` records the immutable source ID, title, closed source/origin/trust values, confined raw path and exact-byte SHA-256 fingerprint, dates, lifecycle, derived references, and creation/update timestamps. Write it through Scrinium source operations; do not repair malformed records as prose.

A record establishes provenance and byte identity. It does not prove semantic correctness or verify claims derived from the source.

## Source Summary Page

Path: `sources/SRC-YYYYMMDD-slug.md`

Required sections:

- `# <Source Title>`
- `## Metadata`: source ID, canonical record, original path, type, dates, trust, and raw fingerprint.
- `## Summary`: concise neutral summary.
- `## Key Claims`: claim IDs where useful.
- `## Entities and Concepts`: affected-page links.
- `## Contradictions or Updates`: conflicts with existing content.
- `## Derived Pages`: created or updated pages.

The Markdown summary is a human-readable derivative, not canonical metadata.

## Entity Page

Include overview, current state, sourced facts, open questions, and related pages.

## Concept Page

Include definition, importance, evidence/examples, related concepts, and open questions.

## Project or Status Page

Include goal, current status, active decisions, next actions, risks/blockers, and references.

## Synthesis Page

Include question/thesis, synthesis, evidence map, alternatives, and confidence/gaps.
