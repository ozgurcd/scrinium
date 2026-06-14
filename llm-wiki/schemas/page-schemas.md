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

Frontmatter is recommended for new generated wiki pages. Preserve existing page style when updating older pages unless a schema migration is explicitly requested.

## Source Page

Path: `sources/SRC-YYYYMMDD-slug.md`

Required sections:

- `# <Source Title>`
- `## Metadata`: source ID, original path, source type, received date, ingest date, trust level.
- `## Summary`: concise neutral summary.
- `## Key Claims`: bullet list with claim IDs if useful.
- `## Entities and Concepts`: links to affected pages.
- `## Contradictions or Updates`: conflicts with existing wiki content.
- `## Derived Pages`: pages created or updated from this source.

## Entity Page

Use for people, organizations, systems, repositories, products, or durable named objects.

Required sections:

- Overview.
- Current state.
- Known facts with source IDs.
- Open questions.
- Related pages.

## Concept Page

Use for recurring ideas, patterns, practices, or technical concepts.

Required sections:

- Definition.
- Why it matters.
- Evidence and examples.
- Related concepts.
- Open questions.

## Project or Status Page

Use for current implementation state.

Required sections:

- Goal.
- Current status.
- Active decisions.
- Next actions.
- Risks or blockers.
- Source or decision references.

## Synthesis Page

Use for durable analysis created from multiple pages or sources.

Required sections:

- Question or thesis.
- Answer or synthesis.
- Evidence map.
- Alternatives considered.
- Confidence and gaps.
