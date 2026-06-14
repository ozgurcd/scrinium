---
title: Project Design: LLM-Wiki MCP Server
type: source
status: current
updated: 2026-06-13
sources:
  - SRC-20260613-project-design
---

# Project Design: LLM-Wiki MCP Server

## Metadata

- Source ID: `SRC-20260613-project-design`
- Original path: `raw/inbox/PROJECT_DESIGN.md`
- Source type: project design document
- Received date: 2026-06-13
- Ingest date: 2026-06-13
- Trust level: `trusted-owner`

## Summary

This source defines Scrinium as a CLI-based Model Context Protocol server for managing a local `llm-wiki` directory. It describes the project as a governed JSON-RPC-over-MCP interface over wiki files, with read access for contextual awareness and tightly scoped write tools for safe updates. The design emphasizes policy-based access control, protected foundational documents, mutable working zones, semantic error messages for blocked writes, and a verification workflow based on Makefile targets.

## Key Claims

- Scrinium is a CLI-based MCP server, not an HTTP-based server. Source: `SRC-20260613-project-design`.
- Scrinium manages a local `llm-wiki` structure for AI coding agents. Source: `SRC-20260613-project-design`.
- The intended implementation language is Go, with standard-library-first filesystem handling. Source: `SRC-20260613-project-design`.
- The server uses JSON-RPC as its MCP interface. Source: `SRC-20260613-project-design`.
- Write governance is configured through `scrinium.json`, including protected file patterns. Source: `SRC-20260613-project-design`.
- Protected writes should fail with semantic, LLM-readable error messages that guide the agent toward drafts. Source: `SRC-20260613-project-design`.
- Exposed tools include `read_wiki_page`, `update_wiki_page`, `create_draft`, and `append_log`. Source: `SRC-20260613-project-design`.
- Completion for code changes requires successful `make test` and `make verify`. Source: `SRC-20260613-project-design`.

## Entities and Concepts

- `projects/scrinium.md`
- `concepts/policy-based-access-control.md`
- `concepts/semantic-rejection.md`

## Contradictions or Updates

- The source says agents must prioritize `~/.gemini/GEMINI.md`, `docs/ARCHITECTURAL_GUIDELINES.md`, and `.agent/rules/`. Current active project guidance uses `AGENTS.md` and `llm-wiki/` pages instead. Treat this as stale source guidance, not an active instruction.
- The source describes CRUD wiki access, but current Scrinium guidance exposes controlled read/update/draft/append operations rather than an unrestricted delete operation.
- The source mentions mutable session logs; current LLM Wiki structure uses canonical `llm-wiki/log.md`, and the previous `llm-wiki/logs/` directory was removed as unnecessary.

## Derived Pages

- `source-registry.md`
- `projects/scrinium.md`
- `concepts/policy-based-access-control.md`
- `concepts/semantic-rejection.md`
- `index.md`
- `log.md`
