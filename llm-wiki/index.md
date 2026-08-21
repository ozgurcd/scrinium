# LLM Wiki Index

## Operating Model

Scrinium records evidence-backed project claims. Stored Markdown and JSON are inspectable records, not automatic truth.

## Core Knowledge

- `claims/` — canonical deterministic claim JSON, one immutable ID per file.
- `sources/records/` — canonical deterministic source provenance JSON.
- `source-registry.md` — generated compatibility view.
- `sources/README.md` — human source-summary guide.
- `projects/scrinium.md` — current Scrinium implementation status and supported Go baseline.

## Architecture and Public Contract

- `architecture/overview.md` — protected accepted overview (currently legacy; update proposed through draft).
- `drafts/architecture-overview-v0.2-final-2026-08-21.md` — proposed final v0.2 protected overview.
- `drafts/go-1.27-protected-governance-2026-08-21.md` — proposed Go 1.27 updates for protected rules/development documents.
- Repository docs: `docs/v0.2-evidence-architecture.md` and `docs/v0.2-public-api.md`.

## Workflows

- `workflows/ingest.md` — canonical source ingestion.
- `workflows/query.md` — evidence-aware knowledge queries.
- `workflows/lint.md` — deterministic lint versus heuristic review.

## Governance and Security

- `agent-rules.md`
- `prompt-templates.md`
- `platform/open-source.md`
- `schemas/page-schemas.md`
- `security/untrusted-sources.md`

## Log

- `log.md` — append-only chronological wiki maintenance log.
