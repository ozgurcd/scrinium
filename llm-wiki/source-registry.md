# Source Registry

This registry tracks raw sources ingested into the wiki.

## Registry Rules

- Every ingested source gets a stable ID: `SRC-YYYYMMDD-slug`.
- The original source file remains under `raw/` and is not modified during ingestion.
- Source summaries live under `sources/<source-id>.md`.
- If a source is superseded, do not delete the old entry. Mark it superseded and link the newer source.

## Sources

### SRC-20260613-project-design

- Title: Project Design: LLM-Wiki MCP Server
- Raw path: `raw/inbox/PROJECT_DESIGN.md`
- Source summary: `sources/SRC-20260613-project-design.md`
- Source type: project design document
- Trust level: `trusted-owner`
- Received date: 2026-06-13
- Ingest date: 2026-06-13
- Status: current
- Derived pages:
  - `projects/scrinium.md`
  - `concepts/policy-based-access-control.md`
  - `concepts/semantic-rejection.md`
- Notes: Contains stale references to `~/.gemini/GEMINI.md`, `docs/ARCHITECTURAL_GUIDELINES.md`, and `.agent/rules/`; active guidance remains `AGENTS.md` plus governed `llm-wiki` pages.
