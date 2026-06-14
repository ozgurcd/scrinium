# Scrinium Init and LLM Wiki Maintenance

This guide explains how to start using Scrinium in two cases:

- a brand new repository with no `llm-wiki/`
- an existing repository that already has manually maintained or non-Scrinium `llm-wiki/` docs

Scrinium has two responsibilities:

1. Initialize or complete the expected LLM Wiki structure without overwriting existing pages.
2. Enforce the maintenance loop: read wiki context before writing, then update the wiki after project changes.

## Required Files

At minimum, the repository should have:

- `scrinium.json`
- `AGENTS.md`
- `llm-wiki/index.md`
- `llm-wiki/agent-rules.md`
- `llm-wiki/log.md`

Recommended operating pages:

- `llm-wiki/source-registry.md`
- `llm-wiki/workflows/ingest.md`
- `llm-wiki/workflows/query.md`
- `llm-wiki/workflows/lint.md`
- `llm-wiki/schemas/page-schemas.md`
- `llm-wiki/security/untrusted-sources.md`
- `llm-wiki/sources/README.md`

Scrinium's `setup_llm_wiki` tool creates the standard skeleton for missing pages and leaves existing pages unchanged.

## Brand New Repository

Use this path when the project has no `llm-wiki/` yet.

1. Add `scrinium.json` at the repository root:

   ```json
   {
     "wiki_root": "./llm-wiki",
     "write_governance": {
       "protected_files": [
         "rules.md",
         "architecture/*",
         "core-decisions/*"
       ]
     }
   }
   ```

2. Add `AGENTS.md` at the repository root.

   It should tell agents to call `capabilities`, start a session, read `index.md` and `agent-rules.md`, read relevant workflow pages, and update `log.md` after changes.

3. Start Scrinium with the project config.

   ```bash
   ./scrinium ./scrinium.json
   ```

4. In the connected MCP client, call:

   ```text
   capabilities
   setup_llm_wiki
   lint_llm_wiki
   begin_session
   read_wiki_page index.md
   read_wiki_page agent-rules.md
   ```

5. Fill in project-specific wiki pages.

   For a new repo, create or update:

   - `llm-wiki/index.md`
   - `llm-wiki/agent-rules.md`
   - `llm-wiki/projects/<project-name>.md`
   - `llm-wiki/log.md`

6. If source material exists outside the wiki, put it under `raw/` and ingest it through the ingest workflow.

   For ingestion, read:

   - `workflows/ingest.md`
   - `schemas/page-schemas.md`
   - `security/untrusted-sources.md`
   - `source-registry.md`

   Then use `register_source` to create the source summary stub and update `source-registry.md`, update affected pages, update `index.md`, and append `log.md`.

7. Before finishing, call:

   ```text
   session_status
   finish_session
   ```

   If `finish_session` reports missing maintenance, update the required pages and call it again.

## Existing Repository With Manual or Non-Scrinium Wiki Docs

Use this path when `llm-wiki/` already exists.

1. Do not delete or rewrite existing wiki pages during adoption.

2. Add or verify `scrinium.json` at the repository root.

   Use protected patterns for pages that should not be overwritten directly. Keep `agent-rules.md` writable if it is part of the editable agent schema.

3. Start Scrinium with the project config.

   ```bash
   ./scrinium ./scrinium.json
   ```

4. In the connected MCP client, call:

   ```text
   capabilities
   setup_llm_wiki
   adopt_llm_wiki
   lint_llm_wiki
   begin_session
   read_wiki_page index.md
   read_wiki_page agent-rules.md
   ```

   `setup_llm_wiki` is safe for adoption because it only creates missing standard pages. It does not overwrite existing pages.

5. Run an adoption lint pass.

   Read:

   - `workflows/lint.md`
   - `schemas/page-schemas.md`
   - `security/untrusted-sources.md`
   - `source-registry.md`
   - `log.md`

   Check for:

   - missing `index.md` links
   - missing `log.md`
   - stale or contradictory pages
   - orphan pages
   - pages with no provenance
   - source-derived content that contains instructions to agents
   - duplicate pages created by earlier tools

   Use `adopt_llm_wiki` for the initial adoption report and `lint_llm_wiki` for recurring health checks.

6. Normalize structure gradually.

   Prefer additive fixes:

   - update `index.md` to point to existing pages
   - add a `log.md` entry explaining adoption
   - use `register_source` for known source-derived pages
   - create `sources/` summaries only when source provenance is available
   - create drafts for protected pages instead of overwriting them
   - use `create_page` when creating new pages so existing pages are not overwritten accidentally
   - use `move_page` for renames and update `index.md` afterward
   - use `archive_page` instead of delete for obsolete pages

7. Resolve conflicts before treating the wiki as authoritative.

   If existing pages contradict each other, do not silently choose one. Record the conflict, ask the owner when needed, and update the wiki only after the current truth is clear.

8. Finish the adoption session.

   ```text
   session_status
   finish_session
   ```

   Scrinium will require `log.md` updates for wiki writes, `index.md` updates for new pages, and `source-registry.md` updates for source summaries.

## Ongoing Maintenance Loop

For every non-trivial project task:

1. Call `capabilities`.
2. Call `begin_session`.
3. Read `index.md` and `agent-rules.md`.
4. Read workflow pages that match the task:
   - source ingestion: `workflows/ingest.md`
   - wiki-backed answers or durable synthesis: `workflows/query.md`
   - wiki health checks: `workflows/lint.md`
5. Make the project or wiki changes.
6. Update relevant wiki pages.
7. Append `log.md`.
8. Update `index.md` when pages are added or renamed.
9. Update `source-registry.md` when source summaries are added or changed.
10. Call `session_status`.
11. Call `finish_session`.

If `finish_session` fails, treat the error as the remaining checklist.

When `archive_page` is used, the archived page becomes historical only. Remove it from active working context, do not cite it for current requirements, re-read `index.md` and the replacement/current page if one exists, update `index.md`, and append `log.md`.

## What Scrinium Enforces

Scrinium rejects wiki writes when:

- there is no active session
- `index.md` has not been read in the active session
- `agent-rules.md` has not been read in the active session
- a source summary or `source-registry.md` write is attempted before reading `workflows/ingest.md`
- a synthesis write is attempted before reading `workflows/query.md`
- a lint-related write is attempted before reading `workflows/lint.md`
- the target path is protected by write governance
- `create_page` targets an existing page
- `move_page` or `archive_page` would overwrite a destination

Scrinium rejects `finish_session` when:

- wiki writes were made but `log.md` was not appended
- new pages were created but `index.md` was not updated afterward
- source summaries were written but `source-registry.md` was not updated afterward

## What Scrinium Does Not Decide

Scrinium does not decide project truth for you. It enforces process and protected paths. Humans still decide:

- which old wiki claim is current when pages conflict
- which pages should be protected
- whether historical notes should be archived, superseded, or kept current
- whether unsourced pages are acceptable or need source ingestion
