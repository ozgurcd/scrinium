# Source Summaries and Records

This directory has two distinct layers:

- `records/SRC-YYYYMMDD-slug.json` contains canonical deterministic source metadata.
- `SRC-YYYYMMDD-slug.md` contains human-readable derivative summaries.

Raw source files remain under `raw/` and must not be modified during ingestion. Scrinium records a full SHA-256 fingerprint of exact raw bytes; changed bytes remain stale until explicitly accepted.

Canonical provenance identifies a source and its bytes. It does not prove the source semantically correct or verify claims derived from it.

Summaries follow `schemas/page-schemas.md`. `source-registry.md` is a generated compatibility view and is not authoritative metadata.
