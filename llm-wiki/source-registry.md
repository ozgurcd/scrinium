# Source Registry

This compatibility view is generated from canonical records under `sources/records/`. Manual edits are not canonical.

## Registry Rules

- Every source has an immutable `SRC-YYYYMMDD-slug` ID.
- Raw bytes remain repository-owned and are identified by a full SHA-256 fingerprint.
- Source provenance does not establish the truth of claims derived from it.

## Sources

### SRC-20260613-project-design

- Title: Project Design: LLM-Wiki MCP Server
- Raw path: `raw/inbox/PROJECT_DESIGN.md`
- Raw fingerprint: `sha256:1a76a50f7fdcc6a2176da47472dd60f2a6d62b48d0639ce7c4486cc28ef41d59`
- Source summary: `sources/SRC-20260613-project-design.md`
- Source type: `project_document`
- Origin: `owner`
- Trust classification: `trusted-owner`
- Received date: 2026-06-13
- Ingest date: 2026-06-13
- Status: `current`
- Derived claims:
  - None
- Derived pages:
  - `concepts/policy-based-access-control.md`
  - `concepts/semantic-rejection.md`
  - `projects/scrinium.md`
- Provenance notes:
  - `Contains stale references to `~/.gemini/GEMINI.md`, `docs/ARCHITECTURAL_GUIDELINES.md`, and `.agent/rules/`; active guidance remains `AGENTS.md` plus governed `llm-wiki` pages.`
