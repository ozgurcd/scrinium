# Scrinium Platform

## Design
Scrinium is an Apache 2.0 open-source MCP server written in Go. It exposes a local `llm-wiki` filesystem through JSON-RPC over stdio (stdin/stdout), providing AI agents with governed read/write access to project context.

## Key Characteristics
- **Single binary** — compiles to a standalone executable, no runtime dependencies.
- **Standard library only** — uses `os`, `path/filepath`, `io/fs`, `bufio`, `encoding/json`.
- **Configuration-driven** — governance rules are defined in `scrinium.json`, not hardcoded.
- **Stdio transport** — reads JSON-RPC from stdin, writes responses to stdout.

## Deployment
1. Build: `make build` (outputs `./scrinium` in project root).
2. Run: `./scrinium ./scrinium.json`.
3. The server reads `scrinium.json` for wiki root path and write governance rules.
4. Agents launch the binary and communicate via stdin/stdout JSON-RPC.

## Governance Model
Policy-Based Access Control (PBAC) enforced at the protocol layer:
- Protected zones are read-only (`rules.md`, `architecture/*`, `core-decisions/*`).
- Agents use `create_draft` to propose changes to protected documents.
- `append_log` is always allowed — it only appends, never overwrites.
