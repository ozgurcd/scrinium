# Scrinium Guide

This generated guide explains how to use this project's wiki.

## Getting Started

1. Call `capabilities` first.
2. Call `setup_llm_wiki` only when no wiki exists.
3. Call `begin_session` and retain its durable session ID.
4. Read `index.md`, `agent-rules.md`, and relevant workflows.
5. Pass the explicit session ID to session-dependent CLI operations; MCP may remember it as connection convenience.
6. Call `finish_session` before reporting completion.

## Knowledge and Provenance

Markdown is human-readable documentation, not automatically true. Canonical claims and source records are strict repository-owned JSON:

- `claims/<CLAIM-ID>.json`
- `sources/records/SRC-YYYYMMDD-slug.json`

Source summaries remain under `sources/*.md`. `source-registry.md` is a deterministic compatibility view rebuilt from canonical records. A source fingerprint identifies exact bytes; provenance does not establish semantic correctness or machine verification.

Sessions are durable tracked work-session checklists. They record only Scrinium-observed operations and are neither authentication credentials nor proof of compliance.

## Tools

- `capabilities`, `setup_llm_wiki`
- `begin_session`, `session_status`, `finish_session`, `abandon_session`, `list_active_sessions`
- compatibility page tools: `read_wiki_page`, `update_wiki_page`, `create_page`, `move_page`, `archive_page`
- `create_draft`, `append_log`, `lint_llm_wiki`, `adopt_llm_wiki`
- `register_source`: verify/fingerprint raw bytes and write canonical metadata before compatibility documents
- `assess_source_migration`: read-only deterministic legacy assessment
- `apply_source_migration`: create safe canonical records without rewriting Markdown
- `rebuild_source_registry`: rebuild the human registry from canonical records

## Governance

Call `capabilities` for live protected-file rules. Before source work, read `workflows/ingest.md`, `schemas/page-schemas.md`, and `security/untrusted-sources.md`. Maintain applicable pages and `log.md`, then satisfy the session checklist.
