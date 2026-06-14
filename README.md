# Scrinium

Scrinium is a Model Context Protocol (MCP) server designed to manage local `llm-wiki` structures. It provides governed read/write access to wiki files, serving as a structured memory layer to keep agents focused on project rules and reduce hallucinations.

## Features

- **Policy-Based Access Control (PBAC)**: Implements strict read/write governance to protect foundational rules
- **Session Enforcement**: Requires agents to start a session, read required wiki context, and finish only after maintenance obligations are complete
- **Immutable Zones**: Protects critical documentation files from unauthorized modifications
- **Mutable Zones**: Allows working with drafts and logs in controlled ways
- **Semantic Error Handling**: Returns human-readable error messages for LLM consumption

## Architecture

Scrinium acts as an agentic governance gateway that:
1. Exposes the entire `llm-wiki` directory as MCP URIs for reading
2. Provides controlled write access through specific tools with governance enforcement
3. Enforces the LLM Wiki read-before-write and update-after-write cycle
4. Prevents context drift by protecting project rules from unintended overwrites

## Configuration

The server is configured via `scrinium.json` which specifies:
- The wiki root directory path
- Write governance rules that define protected files and allowed tools

## MCP Capabilities

### Resources (Read)
- Exposes all wiki files as MCP URIs (`llm-wiki://file/path.md`)

### Tools (Write/Execute)
- `read_wiki_page`: Read a specific wiki page
- `begin_session`: Start a tracked LLM Wiki work session before writes
- `session_status`: Inspect recorded reads, writes, and pending maintenance requirements
- `finish_session`: Verify required log, index, and source-registry updates before completion
- `update_wiki_page`: Modify a wiki page with governance checks
- `create_draft`: Create a draft in the drafts/ directory
- `append_log`: Append content to logs without modifying existing content
- `setup_llm_wiki`: Initialize the standard LLM Wiki structure without overwriting existing pages
- `lint_llm_wiki`: Run a read-only wiki health check for missing pages, index gaps, provenance gaps, and source-instruction risks
- `adopt_llm_wiki`: Run a read-only adoption scan for an existing manual or non-Scrinium wiki
- `register_source`: Register a raw source and create or update its source summary stub
- `create_page`: Create a page only when it does not already exist
- `move_page`: Rename a wiki page without overwriting the destination
- `archive_page`: Move obsolete content under `archive/`; archived content is historical only and must be dropped from active working context

## Documentation

- [Scrinium Init and LLM Wiki Maintenance](docs/scrinium-init-and-maintenance.md): how to initialize a brand new repo, adopt an existing manually maintained `llm-wiki/`, and keep the wiki current through the enforced session loop.

## Build & Run

```bash
# Build
make build

# Run with configuration
./scrinium ./scrinium.json
```

## Development Workflow

1. Read `PROJECT_DESIGN.md` and `AGENTS.md` for project requirements
2. Ensure compliance with architectural guidelines before making changes
3. All development must pass `make test` and `make verify`
4. Changes must be verified through full testing suite before completion
