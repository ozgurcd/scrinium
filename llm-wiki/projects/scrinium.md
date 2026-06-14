---
title: Scrinium
type: project
status: current
updated: 2026-06-14
sources:
  - SRC-20260613-project-design
---

# Scrinium

## Goal

Scrinium is a CLI-based Model Context Protocol server that manages a local `llm-wiki` directory for AI coding agents. It provides governed access to persistent project context so agents can read current rules and update working knowledge without silently corrupting foundational documents.

## Current Status

The project is implemented as a Go MCP server using JSON-RPC over stdio. It is configured through `scrinium.json`, which sets the wiki root and protected file patterns. Current wiki operation follows the LLM Wiki pattern: immutable raw sources, maintained wiki pages, and agent schema through `AGENTS.md` plus workflow/schema/security pages. Release versioning uses SemVer tracked by `.bumpversion.cfg`; `make build` embeds the Makefile `VERSION` into the binary.

The active MCP tool surface includes `capabilities`, `setup_llm_wiki`, `begin_session`, `session_status`, `finish_session`, `read_wiki_page`, `update_wiki_page`, `create_draft`, `append_log`, `lint_llm_wiki`, `adopt_llm_wiki`, `register_source`, `create_page`, `move_page`, and `archive_page`. `setup_llm_wiki` creates the standard LLM Wiki skeleton without overwriting existing pages. Session tools enforce the LLM Wiki loop by requiring startup reads before writes and required maintenance before completion. Adoption tools support real-world manual wiki onboarding, source registration, safe page creation, renames, and archive-over-delete behavior. The binary also has a non-MCP `enforce-agents` CLI subcommand for manually creating or refreshing Scrinium-managed enforcement blocks in `AGENTS.md`, `CLAUDE.md`, and `docs/scrinium-agent-enforcement.md`.

## Active Decisions

- Use Go and prefer standard-library filesystem operations where possible. Source: `SRC-20260613-project-design`.
- Use `scrinium.json` for policy-based write governance. Source: `SRC-20260613-project-design`.
- Protect foundational documents and route proposed changes through drafts. Source: `SRC-20260613-project-design`.
- Do not protect `agent-rules.md` in the default project config; it is part of the editable agent schema.
- Enforce the LLM Wiki read-before-write and update-after-write cycle in Scrinium itself through tracked sessions.
- Provide `scrinium enforce-agents` as a manual CLI path that does not start MCP stdio mode and preserves user content outside Scrinium-managed blocks.
- Embed the Makefile `VERSION` at compile time and expose it through `scrinium version`, MCP initialize metadata, and the `capabilities` tool.
- Use read-only lint/adoption reports for existing wiki onboarding before making normalizing changes.
- Archive obsolete pages instead of deleting them; archived content is historical and must be removed from active working context.
- Use semantic rejection messages for blocked writes so agents can self-correct. Source: `SRC-20260613-project-design`.
- Use `llm-wiki/log.md` as the canonical chronological LLM Wiki log. Source: `AGENTS.md` and `index.md`.

## Next Actions

- Keep source-derived project facts tied to source IDs.
- If protected architecture pages need updates from this source, propose drafts rather than direct edits.
- Use `workflows/lint.md` for future periodic health checks.

## Risks or Blockers

- `SRC-20260613-project-design` includes stale agent-rule references to `~/.gemini/GEMINI.md`, `docs/ARCHITECTURAL_GUIDELINES.md`, and `.agent/rules/`. Active instructions come from `AGENTS.md` and governed wiki pages.
- The source says CRUD, but current governance does not expose unrestricted delete semantics.

## Source or Decision References

- `sources/SRC-20260613-project-design.md`
- `source-registry.md`
- `concepts/policy-based-access-control.md`
- `concepts/semantic-rejection.md`
