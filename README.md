# Scrinium

Scrinium is a Model Context Protocol (MCP) server designed to manage local `llm-wiki` structures. It provides an interface for AI coding agents to Create, Read, Update, and Delete (CRUD) wiki files through code, serving as a structured memory layer to keep agents focused on project rules and reduce hallucinations.

## Features

- **Policy-Based Access Control (PBAC)**: Implements strict read/write governance to protect foundational rules
- **Immutable Zones**: Protects critical documentation files from unauthorized modifications  
- **Mutable Zones**: Allows working with drafts and logs in controlled ways
- **Semantic Error Handling**: Returns human-readable error messages for LLM consumption

## Architecture

Scrinium acts as an agentic governance gateway that:
1. Exposes the entire `llm-wiki` directory as MCP URIs for reading
2. Provides controlled write access through specific tools with governance enforcement
3. Prevents context drift by protecting project rules from unintended overwrites

## Configuration

The server is configured via `opencode.json` which specifies:
- The wiki root directory path
- Write governance rules that define protected files and allowed tools

## MCP Capabilities

### Resources (Read)
- Exposes all wiki files as MCP URIs (`llm-wiki://file/path.md`)

### Tools (Write/Execute) 
- `read_wiki_page`: Read a specific wiki page
- `update_wiki_page`: Modify a wiki page with governance checks
- `create_draft`: Create a draft in the drafts/ directory
- `append_log`: Append content to logs without modifying existing content

## Build & Run

```bash
# Build
make build

# Run with configuration
./build/scrinium ./opencode.json
```

## Development Workflow

1. Read `PROJECT_DESIGN.md` and `AGENTS.md` for project requirements
2. Ensure compliance with architectural guidelines before making changes
3. All development must pass `make test` and `make verify` 
4. Changes must be verified through full testing suite before completion