# Scrinium Guide

This file was created automatically by Scrinium. It tells you how to use this project's wiki.

## Getting Started

1. Call the `capabilities` tool first. It returns what this server can do, what tools are available, and what governance rules apply.
2. If a project does not have an LLM Wiki yet, call `setup_llm_wiki` to create the standard structure.
3. Call `begin_session` before project changes.
4. Read `index.md` and `agent-rules.md` to understand the wiki structure, current state, and active rules.
5. Call `finish_session` before reporting completion.

## How to Use the Wiki

The llm-wiki is your persistent memory. Use it constantly — not just at startup.

- **Before making changes:** Read relevant wiki pages to understand existing context, decisions, and rules. Do not assume you know the current state.
- **After making changes:** Update the relevant wiki pages to reflect what you did. If you made a decision, record it. If you changed architecture, update the docs.
- **When you learn something:** If you discover project patterns, constraints, or gotchas that are not documented, write them to the appropriate page so the next agent benefits.
- **Before writing:** Scrinium requires an active session plus recorded reads of `index.md` and `agent-rules.md`.
- **After writing:** Scrinium requires `log.md` maintenance and may require `index.md` or `source-registry.md` maintenance before the session can finish.

## Tools

- `capabilities` — Call this FIRST. Returns server info, available tools, and active governance rules.
- `setup_llm_wiki` — Initialize the standard LLM Wiki structure when a project does not have one. Existing pages are left unchanged.
- `begin_session` — Start a tracked work session. Required before wiki writes.
- `session_status` — Show pages read, pages written, and pending maintenance requirements.
- `finish_session` — Verify required log, index, and source-registry updates before completion.
- `read_wiki_page` — Read any wiki page. No restrictions.
- `update_wiki_page` — Write a wiki page. Blocked for protected files.
- `create_draft` — Propose changes to protected files via the drafts/ directory.
- `append_log` — Append text to a log file. Append-only, bypasses governance except for directly protected files.
- `lint_llm_wiki` — Run a read-only health check for missing standard pages, index gaps, provenance gaps, and source-instruction risks.
- `adopt_llm_wiki` — Run a read-only adoption scan for an existing manual or non-Scrinium wiki.
- `register_source` — Register a raw source and create or update its source summary stub. Requires ingest workflow context.
- `create_page` — Create a new page only if it does not already exist.
- `move_page` — Rename a page within the wiki root without overwriting the destination.
- `archive_page` — Move obsolete content under `archive/`. After archiving, treat that content as historical only and remove it from active working context.

## Write Governance

Some files are protected and cannot be modified directly. If you try, you will receive a semantic error explaining what happened and what to do instead. Follow that guidance.

To see which files are protected, call `capabilities` — it returns the live governance rules.
