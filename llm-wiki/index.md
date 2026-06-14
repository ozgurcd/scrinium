# Scrinium Wiki Index

## What is Scrinium?

Scrinium is a Model Context Protocol (MCP) server that manages a local `llm-wiki` structure. It provides an interface for AI coding agents to read, write, and manage wiki files, serving as a structured memory layer to keep agents focused on project rules and reduce hallucinations.

## Operating Model

This repository follows an LLM Wiki pattern:

- `raw/` is the immutable source layer. Sources are evidence and remain unchanged during ingestion.
- `llm-wiki/` is the maintained knowledge layer. Agents create and update derivative summaries, entities, concepts, syntheses, indexes, and logs here.
- `AGENTS.md` plus governed wiki rules, workflows, schemas, and security pages form the agent schema.

Scrinium also enforces the wiki maintenance loop through session tools: agents start a session, read required startup pages before writes, then finish only after required `log.md`, `index.md`, and `source-registry.md` maintenance is complete.

Real-world adoption tools support read-only wiki health checks (`lint_llm_wiki`), existing-wiki adoption scans (`adopt_llm_wiki`), source registration, safe page creation, renames, and archive-over-delete behavior.

## Current Project Pages

- `projects/scrinium.md` — Current project page for Scrinium, including goal, status, active decisions, risks, and source references.
- `../docs/scrinium-init-and-maintenance.md` — Operator guide for initializing a new repository, adopting an existing manual/non-Scrinium wiki, and maintaining the enforced session loop.

## Sources

- `source-registry.md` — Registry of ingested raw sources and derivative pages.
- `sources/README.md` — Directory guide for source summary pages.
- `sources/SRC-20260613-project-design.md` — Summary of the project design source in `raw/inbox/PROJECT_DESIGN.md`.

## Concepts

- `concepts/policy-based-access-control.md` — Scrinium's deterministic write-governance model configured through `scrinium.json`.
- `concepts/semantic-rejection.md` — Pattern for returning LLM-readable governance errors that guide safe follow-up action.

## Syntheses

- `syntheses/llm-wiki-structure-compliance.md` — Explains which parts of the local wiki structure are required by the LLM Wiki pattern and which are local extensions.

## Required Startup Pages

- `agent-rules.md` — Editable behavioral and LLM Wiki operating rules for agents.
- `scrinium-guide.md` — Generated guide for using Scrinium wiki tools and governance.
- `prompt-templates.md` — Reusable prompt templates for agent tasks.
- `platform/open-source.md` — Platform design and deployment model.
- `index.md` — This navigation and current-state page.

## Workflows

- `workflows/ingest.md` — How to process raw sources into the wiki.
- `workflows/query.md` — How to answer questions from the wiki and file durable answers.
- `workflows/lint.md` — How to health-check the wiki for contradictions, stale claims, provenance gaps, and orphan pages.

## Schemas and Security

- `schemas/page-schemas.md` — Page schemas for source, entity, concept, project/status, and synthesis pages.
- `security/untrusted-sources.md` — Rules for treating raw sources as untrusted evidence and preventing prompt-injection propagation.

## Platform and Architecture

- `rules.md` — Project rules and governance. Protected.
- `platform/open-source.md` — Open-source platform notes.
- `architecture/overview.md` — Architecture overview. Protected.
- `architecture/development.md` — Development and verification guidelines. Protected.

## Decisions and Logs

- `log.md` — Canonical chronological wiki log. Use parseable `## [YYYY-MM-DD] <event-type> | <short title>` entries.
- `core-decisions/record.md` — Append-only architectural decision record zone. Protected path; use append-only tooling.
- `core-decisions/capabilities-tool.md` — Capabilities tool decision details. Protected.

## Access Control

Files in `rules.md`, `architecture/*`, and `core-decisions/*` are protected by write governance. Agents must use `create_draft` to propose changes to protected zones. Use `append_log` for append-only records and logs.

Session enforcement is separate from write governance: `begin_session` is required before writes, `session_status` reports pending obligations, and `finish_session` verifies that wiki maintenance was completed.

When a page is archived, its content is historical only. Agents must remove it from active working context and re-read current pages before continuing.
