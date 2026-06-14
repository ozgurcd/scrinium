---
title: LLM Wiki Structure Compliance
type: synthesis
status: current
updated: 2026-06-13
sources:
  - SRC-20260613-project-design
---

# LLM Wiki Structure Compliance

## Question or Thesis

What repository structure is required to follow the LLM Wiki pattern described by Karpathy's LLM Wiki gist, and do any current `llm-wiki/` directories violate that pattern?

## Answer or Synthesis

The gist defines a pattern, not a rigid directory specification. It requires three conceptual layers: immutable raw sources, a maintained wiki of generated markdown pages, and an agent schema that explains structure and workflows. It specifically calls out `index.md` as the content catalog and `log.md` as the chronological append-only timeline.

The gist does not forbid additional wiki subdirectories. Current directories such as `sources/`, `projects/`, `concepts/`, `workflows/`, `schemas/`, `security/`, `drafts/`, `architecture/`, `core-decisions/`, and `platform/` are local schema/governance choices. They are acceptable as long as `index.md` catalogs them, `log.md` records meaningful activity, and source-derived claims preserve provenance.

The previous `logs/` directory was unnecessary after `log.md` became canonical, so it was removed. `log.md` is now the only chronological LLM Wiki log path.

## Evidence Map

- Gist pattern: raw sources are immutable evidence, the wiki is generated markdown, and the schema tells the LLM conventions and workflows.
- Gist special files: `index.md` is the content catalog; `log.md` is the chronological append-only record.
- Current local structure: `AGENTS.md`, `index.md`, `log.md`, workflow pages, schema pages, security pages, source summaries, and registry implement the pattern.
- Local governance: Scrinium protects selected foundational pages and uses drafts for proposed changes; this is stricter than the gist, but not incompatible with the pattern.

## Alternatives Considered

- Remove all directories not named in the gist: rejected because the gist explicitly leaves exact structure to the user and domain.
- Keep `llm-wiki/logs/`: rejected after `log.md` became canonical and historical continuity was already represented in `log.md`.
- Keep both `logs/` and `log.md` as equal logs: rejected because `log.md` should remain canonical.

## Confidence and Gaps

Confidence is high. The remaining gap is not directory structure; it is continued operation: future ingests, durable queries, and lint passes must keep `index.md`, `log.md`, `source-registry.md`, and derived pages current.
